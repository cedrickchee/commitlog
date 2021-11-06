package server

import (
	"context"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	api "github.com/cedrickchee/commitlog/api/v1"
)

// CommitLog allow service to use any given log implementation that satisfies
// this interface.
//
// **Dependency inversion with interfaces** is good practice.
// It would be best if our service weren't tied to a specific log
// implementation. Instead, we want to pass in a log implementation based on our
// needs at the time. We can make this possible by having our service depend on
// a log interface rather than on a concrete type. That way, the service can use
// any log implementation that satisfies the log interface.
type CommitLog interface {
	Append(*api.Record) (uint64, error)
	Read(uint64) (*api.Record, error)
}

type Authorizer interface {
	Authorize(subject, object, action string) error
}

type Config struct {
	// Define the log field that's on our service such that we can pass in
	// different log implementations and make the service easier to write tests
	// against.
	CommitLog CommitLog
	// We depend on an interface for the `Authorizer` so that we can switch out
	// the authorization implementation.
	Authorizer Authorizer
}

// Constants for authorization.
const (
	objectWildcard = "*"
	produceAction  = "produce"
	consumeAction  = "consume"
)

var _ api.LogServer = (*grpcServer)(nil)

// grpcServer defines our server
type grpcServer struct {
	api.UnimplementedLogServer
	*Config
}

// newgrpcServer is a factory function to create an instance of the server.
func newgrpcServer(config *Config) (*grpcServer, error) {
	// Our server depends on a log abstraction. We can pass in a concrete log
	// implementation to the config.CommitLog field.
	//
	// For example, when running in a production environment--where we need our
	// service to persist our user's data--the service will depend on our
	// library. But when running in a test environment, where we don't need to
	// persist our test data, we could use a naive, in-memory log. An in-memory
	// log would also be good for testing because it would make the tests run
	// faster.
	srv := &grpcServer{
		Config: config,
	}
	return srv, nil
}

// NewGRPCServer enables our users to instantiate a new service, create a gRPC
// server, and register our service to that server.
func NewGRPCServer(config *Config, grpcOpts ...grpc.ServerOption) (*grpc.Server, error) {
	// Specify the logger's name to differentiate the server logs from other
	// logs in our service.
	logger := zap.L().Named("server")
	// Add a "grpc.time_ns" field to our structured logs to log the duration of
	// each request in nanoseconds.
	zapOpts := []grpc_zap.Option{
		grpc_zap.WithDurationField(
			func(duration time.Duration) zapcore.Field {
				return zap.Int64(
					"grpc.time_ns",
					duration.Nanoseconds(),
				)
			},
		),
	}

	// Configure how OpenCensus collects metrics and traces.
	//
	// Always sample the traces because we're developing our service and we want
	// all of our requests traced.
	//
	// TODO (ced): going production we may not want to trace every request
	// because it could affect perf, require too much data, or trace
	// confidential data. We can use the probability sampler.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	// The views specify what stats OpenCensus will collect. The default server
	// views track stats on: received bytes per RPC, sent bytes per RPC,
	// latency, and completed RPCs.
	err := view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		return nil, err
	}
	// However, one problem with using the probability sampler is that we may
	// miss important requests. We could try to reconcile these trade-offs by
	// writing our own sampler that always traces important requests and samples
	// a percentage of the rest of the requests. The code for that would look
	// like this:
	//
	// ```go
	// halfSampler := trace.ProbabilitySampler(0.5)
	// trace.ApplyConfig(trace.Config {
	//   DefaultSampler: func(p trace.SamplingParameters) trace.SamplingDecision {
	// 	if strings.Contains(p.Name, "Produce") {
	// 	  return trace.SamplingDecision{Sample: true}
	// 	}
	// 	return halfSampler(p)
	//   },
	// })
	// ```

	// Wire up our `authenticate` interceptor to our gRPC server so that our
	// server identifies the subject of each RPC to kick off the authorization
	// process.
	//
	// Configure gRPC to apply the Zap interceptors that log the gRPC calls.
	grpcOpts = append(grpcOpts,
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(),
			grpc_zap.StreamServerInterceptor(logger, zapOpts...),
			grpc_auth.StreamServerInterceptor(authenticate),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(),
			grpc_zap.UnaryServerInterceptor(logger, zapOpts...),
			grpc_auth.UnaryServerInterceptor(authenticate),
		)),
		// attach OpenCensus as the server's stat handler so that OpenCensus can
		// record stats on the server's request handling.
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)

	gsrv := grpc.NewServer(grpcOpts...)
	srv, err := newgrpcServer(config)
	if err != nil {
		return nil, err
	}
	api.RegisterLogServer(gsrv, srv)
	return gsrv, nil
}

// Produce handles the requests made by clients to produce to the server's log.
func (s *grpcServer) Produce(ctx context.Context, req *api.ProduceRequest) (*api.ProduceResponse, error) {
	// Check whether the client (identified by the cert's subject) is authorized
	// to produce, and if not, send the permission denied error back to the
	// client.
	if err := s.Authorizer.Authorize(
		subject(ctx),
		objectWildcard,
		produceAction,
	); err != nil {
		return nil, err
	}

	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}
	return &api.ProduceResponse{Offset: offset}, nil
}

// Consume handles the requests made by clients to consume to the server's log.
func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (*api.ConsumeResponse, error) {
	// Check whether the client (identified by the cert's subject) is authorized
	// to consume, and if not, send the permission denied error back to the
	// client.
	if err := s.Authorizer.Authorize(
		subject(ctx),
		objectWildcard,
		consumeAction,
	); err != nil {
		return nil, err
	}

	record, err := s.CommitLog.Read(req.Offset)
	if err != nil {
		return nil, err
	}
	return &api.ConsumeResponse{Record: record}, nil
}

// ProduceStream implements a bidirectional streaming RPC so the client can
// stream data into the server's log and the server can tell the client whether
// each request succeeded.
func (s *grpcServer) ProduceStream(stream api.Log_ProduceStreamServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		res, err := s.Produce(stream.Context(), req)
		if err != nil {
			return err
		}
		if err = stream.Send(res); err != nil {
			return err
		}
	}
}

// ConsumeStream implements a server-side streaming RPC so the client can tell
// the server where in the log to read records, and then the server will stream
// every record that follows—even records that aren't in the log yet! When the
// server reaches the end of the log, the server will wait until someone appends
// a record to the log and then continue streaming records to the client.
func (s *grpcServer) ConsumeStream(req *api.ConsumeRequest, stream api.Log_ConsumeStreamServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			res, err := s.Consume(stream.Context(), req)
			// Because our error is a struct type, we can type-switch the error
			// returned by the Consume method (actually, it's Read method).
			switch err.(type) {
			case nil:
			case api.ErrOffsetOutOfRange:
				// Use this error to know whether the server has read to the end
				// of the log.
				continue
			default:
				return err
			}
			if err = stream.Send(res); err != nil {
				return err
			}
			req.Offset++
		}
	}
}

type subjectContextKey struct{}

// subject returns the client's cert's subject so we can identify a client and
// check their access.
func subject(ctx context.Context) string {
	return ctx.Value(subjectContextKey{}).(string)
}

// authenticate is an interceptor (aka. middleware) that reads the subject out
// of the client's cert and writes it to the RPC's context.
func authenticate(ctx context.Context) (context.Context, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.New(codes.Unknown, "couldn't find peer info").Err()
	}

	if peer.AuthInfo == nil {
		return context.WithValue(ctx, subjectContextKey{}, ""), nil
	}

	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	subject := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	ctx = context.WithValue(ctx, subjectContextKey{}, subject)

	return ctx, nil
}
