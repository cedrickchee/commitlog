package loadbalance_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	api "github.com/cedrickchee/commitlog/api/v1"
	"github.com/cedrickchee/commitlog/internal/config"
	"github.com/cedrickchee/commitlog/internal/loadbalance"
	"github.com/cedrickchee/commitlog/internal/server"
)

// TestResolver
func TestResolver(t *testing.T) {
	// Set up a server for our test resolver to try and discover some servers
	// from.

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	tlsConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	serverCreds := credentials.NewTLS(tlsConfig)

	srv, err := server.NewGRPCServer(&server.Config{
		// Pass in a mock so we can set what servers the resolver should find.
		GetServerer: &getServers{},
	}, grpc.Creds(serverCreds))
	require.NoError(t, err)

	go srv.Serve(l)

	// Create and builds the test resolver and configures its target endpoint to
	// point to the server we set up above. The resolver will call
	// `GetServers()` to resolve the servers and update the client connection
	// with the servers' addresses.

	conn := &clientConn{}
	tlsConfig, err = config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.RootClientCertFile,
		KeyFile:       config.RootClientKeyFile,
		CAFile:        config.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	clientCreds := credentials.NewTLS(tlsConfig)
	opts := resolver.BuildOptions{
		DialCreds: clientCreds,
	}

	r := &loadbalance.Resolver{}
	_, err = r.Build(
		resolver.Target{
			Endpoint: l.Addr().String(),
		},
		conn,
		opts,
	)
	require.NoError(t, err)

	// Check that the resolver updated the client connection with the servers
	// and data we expected. We wanted the resolver to find two servers and mark
	// the `9001` server as the leader.

	wantState := resolver.State{
		Addresses: []resolver.Address{
			{
				Addr:       "localhost:9001",
				Attributes: attributes.New("is_leader", true),
			},
			{
				Addr:       "localhost:9002",
				Attributes: attributes.New("is_leader", false),
			},
		},
	}
	require.Equal(t, wantState, conn.state)

	conn.state.Addresses = nil
	r.ResolveNow(resolver.ResolveNowOptions{})
	require.Equal(t, wantState, conn.state)
}

// Our test depended on some mock types.

// getServers is a mock `GetServerers` and implements `GetServerers`.
type getServers struct{}

// GetServers job is to return a known server set for the resolver to find.
func (s *getServers) GetServers() ([]*api.Server, error) {
	// Mock Raft's server data.
	servers := []*api.Server{
		{
			Id:       "leader",
			RpcAddr:  "localhost:9001",
			IsLeader: true,
		},
		{
			Id:      "follower",
			RpcAddr: "localhost:9002",
		},
	}

	return servers, nil
}

// clientConn implements `resolver.ClientConn`, and its job is to keep a
// reference to the state the resolver updated it with so that we can verify
// that the resolver updates the client connection with the correct data.
type clientConn struct {
	resolver.ClientConn
	state resolver.State
}

// UpdateState updates the state of the clientConn appropriately.
func (c *clientConn) UpdateState(state resolver.State) error {
	c.state = state

	return nil
}

// ReportError notifies the ClientConn that the Resolver encountered an error.
func (c *clientConn) ReportError(err error) {}

// Deprecated: Use UpdateState instead.
func (c *clientConn) NewAddress(addresses []resolver.Address) {}

// Deprecated: Use UpdateState instead.
func (c *clientConn) NewServiceConfig(config string) {}

// ParseServiceConfig parses the provided service config and returns an object
// that provides the parsed config.
func (c *clientConn) ParseServiceConfig(
	configJSON string,
) *serviceconfig.ParseResult {
	return nil
}
