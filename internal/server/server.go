package server

import (
	"context"

	api "github.com/cedrickchee/commitlog/api/v1"
	"google.golang.org/grpc"
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

type Config struct {
	// Define the log field that's on our service such that we can pass in
	// different log implementations and make the service easier to write tests
	// against.
	CommitLog CommitLog
}

var _ api.LogServer = (*grpcServer)(nil)

// grpcServer defines our server
type grpcServer struct {
	api.UnimplementedLogServer
	*Config
}

// newgrpcServer is a factory function to create an instance of the server.
func newgrpcServer(config *Config) (srv *grpcServer, err error) {
	// Our server depends on a log abstraction. We can pass in a concrete log
	// implementation to the config.CommitLog field.
	//
	// For example, when running in a production environment--where we need our
	// service to persist our user's data--the service will depend on our
	// library. But when running in a test environment, where we don't need to
	// persist our test data, we could use a naive, in-memory log. An in-memory
	// log would also be good for testing because it would make the tests run
	// faster.
	srv = &grpcServer{
		Config: config,
	}
	return srv, nil
}

// NewGRPCServer enables our users to instantiate a new service, create a gRPC
// server, and register our service to that server.
func NewGRPCServer(config *Config, opts ...grpc.ServerOption) (*grpc.Server, error) {
	gsrv := grpc.NewServer(opts...)
	srv, err := newgrpcServer(config)
	if err != nil {
		return nil, err
	}
	api.RegisterLogServer(gsrv, srv)
	return gsrv, nil
}

// Produce handles the requests made by clients to produce to the server's log.
func (s *grpcServer) Produce(ctx context.Context, req *api.ProduceRequest) (*api.ProduceResponse, error) {
	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}
	return &api.ProduceResponse{Offset: offset}, nil
}

// Consume handles the requests made by clients to consume to the server's log.
func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (*api.ConsumeResponse, error) {
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
