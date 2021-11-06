package server

import (
	"context"
	"flag"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opencensus.io/examples/exporter"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	api "github.com/cedrickchee/commitlog/api/v1"
	"github.com/cedrickchee/commitlog/internal/auth"
	"github.com/cedrickchee/commitlog/internal/config"
	"github.com/cedrickchee/commitlog/internal/log"
)

// A debug flag to enable observability output.
var debug = flag.Bool("debug", false, "Enable observability for debugging.")

// TestMain gives us a place for setup that applies to all tests in this file,
// like enabling our debug output.
func TestMain(m *testing.M) {
	// Note: flag parsing has to go here instead of `init()`
	flag.Parse()
	if *debug {
		logger, err := zap.NewDevelopment()
		if err != nil {
			panic(err)
		}
		zap.ReplaceGlobals(logger)
	}
	os.Exit(m.Run())
}

// TestServer defines a list of test cases and then runs a subtest for each
// case.
func TestServer(t *testing.T) {
	tcases := map[string]func(
		t *testing.T,
		rootClient api.LogClient,
		nobodyClient api.LogClient,
		config *Config,
	){
		"produce/consume a message to/from the log succeeeds": testProduceConsume,
		"produce/consume stream succeeds":                     testProduceConsumeStream,
		"consume past log boundary fails":                     testConsumePastBoundary,
		"unauthorized fails":                                  testUnauthorized,
	}
	for scenario, fn := range tcases {
		t.Run(scenario, func(t *testing.T) {
			rootClient, nobodyClient, config, teardown := setupTest(t, nil)
			defer teardown()
			// Pass both root and nobody clients to the test functions so they
			// can use whatever client they need based on whether they're
			// testing how the server works with an authorized or unauthorized
			// client.
			fn(t, rootClient, nobodyClient, config)
		})
	}
}

// setupTest is a helper function to set up each test case.
func setupTest(t *testing.T, fn func(*Config)) (
	rootClient api.LogClient,
	nobodyClient api.LogClient,
	cfg *Config,
	teardown func(),
) {
	t.Helper()

	// Create a listener on the local network address that our server will run
	// on.
	//
	// The `0` port is useful for when we don't care what port we use since `0`
	// will automatically assign us a free port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Helper function
	newClient := func(crtPath, keyPath string) (
		*grpc.ClientConn,
		api.LogClient,
		[]grpc.DialOption,
	) {
		// Configure our client's TLS credentials to use our CA as the client's
		// Root CA (the CA it will use to verify the server).
		tlsConfig, err := config.SetupTLSConfig(config.TLSConfig{
			CertFile: crtPath,
			KeyFile:  keyPath,
			CAFile:   config.CAFile,
			Server:   false,
		})
		require.NoError(t, err)
		tlsCreds := credentials.NewTLS(tlsConfig)
		// Tell the client to use those credentials for its connection.
		// Make a secure connection to our listener and, with it, a client we'll
		// use to hit our server with.
		opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
		conn, err := grpc.Dial(l.Addr().String(), opts...)
		require.NoError(t, err)
		client := api.NewLogClient(conn)
		return conn, client, opts
	}

	// Build two clients for testing our authorization setup.
	//
	// A superuser client who's permitted to produce and consume.
	var rootConn *grpc.ClientConn
	rootConn, rootClient, _ = newClient(
		config.RootClientCertFile,
		config.RootClientKeyFile,
	)
	// A nobody client who isn't permitted to do anything.
	var nobodyConn *grpc.ClientConn
	nobodyConn, nobodyClient, _ = newClient(
		config.NobodyClientCertFile,
		config.NobodyClientKeyFile,
	)

	// Hook up our server with its certificate and enable it to handle TLS
	// connections.
	serverTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		ServerAddress: l.Addr().String(),
		Server:        true,
	})
	require.NoError(t, err)
	serverCreds := credentials.NewTLS(serverTLSConfig)

	// Create our server
	dir, err := ioutil.TempDir("", "server_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)

	authorizer, err := auth.New(config.ACLModelFile, config.ACLPolicyFile)
	require.NoError(t, err)

	// Set up and start the telemetry exporter to write to two files. Each test
	// gets its own separate trace and metrics files so we can see each test's
	// requests.
	var telemetryExporter *exporter.LogExporter
	if *debug {
		metricsLogFile, err := ioutil.TempFile("", "metrics-*.log")
		require.NoError(t, err)
		t.Logf("metrics log file: %s", metricsLogFile.Name())

		tracesLogFile, err := ioutil.TempFile("", "traces-*.log")
		require.NoError(t, err)
		t.Logf("traces log file: %s", tracesLogFile.Name())

		telemetryExporter, err = exporter.NewLogExporter(exporter.Options{
			MetricsLogFile:    metricsLogFile.Name(),
			TracesLogFile:     tracesLogFile.Name(),
			ReportingInterval: time.Second,
		})
		require.NoError(t, err)
		err = telemetryExporter.Start()
		require.NoError(t, err)
	}

	cfg = &Config{
		CommitLog:  clog,
		Authorizer: authorizer,
	}
	if fn != nil {
		fn(cfg)
	}
	// Configure the server's TLS credentials by pass those credentials as a
	// gRPC ServerOption.
	server, err := NewGRPCServer(cfg, grpc.Creds(serverCreds))
	require.NoError(t, err)

	// Serve requests in a goroutine because the `Serve` method is a blocking
	// call, and if we didn't run it in a goroutine our tests further down would
	// never run.
	go func() {
		server.Serve(l)
	}()

	return rootClient, nobodyClient, cfg, func() {
		server.Stop()
		rootConn.Close()
		nobodyConn.Close()
		l.Close()
		// Sleep for 1.5 seconds to give the telemetry exporter enough time to
		// flush its data to disk. Then stop and close the exporter.
		if telemetryExporter != nil {
			time.Sleep(1500 * time.Millisecond)
			telemetryExporter.Stop()
			telemetryExporter.Close()
		}
	}
}

// testProduceConsume tests that producing and consuming works by using our
// client and server to produce a record to the log, consume it back, and then
// check that the record we sent is the same one we got back.
func testProduceConsume(t *testing.T, client, _ api.LogClient, config *Config) {
	ctx := context.Background()

	want := &api.Record{
		Value: []byte("Hey, this is a test"),
	}
	produce, err := client.Produce(ctx, &api.ProduceRequest{Record: want})
	require.NoError(t, err)

	consume, err := client.Consume(ctx, &api.ConsumeRequest{Offset: produce.Offset})
	require.NoError(t, err)
	require.Equal(t, want.Value, consume.Record.Value)
	require.Equal(t, want.Offset, consume.Record.Offset)
}

// testProduceConsumeStream is the streaming counterpart to
// `testProduceConsume()`, testing that we can produce and consume through
// streams.
func testProduceConsumeStream(t *testing.T, client, _ api.LogClient, config *Config) {
	ctx := context.Background()

	records := []*api.Record{
		{Value: []byte("first message"), Offset: 0},
		{Value: []byte("second message"), Offset: 1},
	}

	{
		stream, err := client.ProduceStream(ctx)
		require.NoError(t, err)

		for offset, record := range records {
			err = stream.Send(&api.ProduceRequest{
				Record: record,
			})
			require.NoError(t, err)

			res, err := stream.Recv()
			require.NoError(t, err)
			if res.Offset != uint64(offset) {
				t.Fatalf("got offset: %d, want: %d", res.Offset, offset)
			}
		}
	}

	{
		stream, err := client.ConsumeStream(ctx, &api.ConsumeRequest{Offset: 0})
		require.NoError(t, err)

		for i, record := range records {
			res, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(
				t,
				&api.Record{
					Value:  record.Value,
					Offset: uint64(i),
				},
				res.Record,
			)
		}
	}
}

// testConsumePastBoundary tests that our server responds with an
// `api.ErrOffsetOutOfRange()` error when a client tries to consume beyond the
// log's boundaries.
func testConsumePastBoundary(t *testing.T, client, _ api.LogClient, config *Config) {
	ctx := context.Background()

	produce, err := client.Produce(ctx, &api.ProduceRequest{
		Record: &api.Record{
			Value: []byte("Hey, this is a test"),
		},
	})
	require.NoError(t, err)

	consume, err := client.Consume(ctx, &api.ConsumeRequest{
		Offset: produce.Offset + 1,
	})
	if consume != nil {
		t.Fatal("consume not nil")
	}
	got := status.Code(err)
	want := status.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err())
	if got != want {
		t.Fatalf("got err: %v, want: %v", got, want)
	}
}

// testUnauthorized checks that unauthorized clients are denied.
func testUnauthorized(t *testing.T, _, client api.LogClient, config *Config) {
	ctx := context.Background()

	// Use the nobody client, which isn't permitted to do anything.
	produce, err := client.Produce(ctx, &api.ProduceRequest{
		Record: &api.Record{
			Value: []byte("Hey, this is a test"),
		},
	})
	if produce != nil {
		t.Fatalf("produce response should be nil")
	}
	// Since our client isn't authorized, we want our server to deny the client,
	// which we verify by checking the code on the returned error.
	gotCode, wantCode := status.Code(err), codes.PermissionDenied
	if gotCode != wantCode {
		t.Fatalf("got code: %d, want: %d", gotCode, wantCode)
	}

	consume, err := client.Consume(ctx, &api.ConsumeRequest{
		Offset: 0,
	})
	if consume != nil {
		t.Fatalf("consume response should be nil")
	}
	gotCode, wantCode = status.Code(err), codes.PermissionDenied
	if gotCode != wantCode {
		t.Fatalf("got code: %d, want: %d", gotCode, wantCode)
	}
}
