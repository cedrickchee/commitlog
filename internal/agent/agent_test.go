package agent_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	api "github.com/cedrickchee/commitlog/api/v1"
	"github.com/cedrickchee/commitlog/internal/agent"
	"github.com/cedrickchee/commitlog/internal/config"
	"github.com/cedrickchee/commitlog/pkg/freeport"
)

// TestAgent tests that service discovery & replication works in an end-to-end
// test.
func TestAgent(t *testing.T) {
	// Defines the certificate configurations used in our test to test our
	// security.

	// Defines the configuration of the certificate that's served to clients.
	serverTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	// Defines the configuration of the certificate that's served between
	// servers so they can connect with and replicate each other.
	peerTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.RootClientCertFile,
		KeyFile:       config.RootClientKeyFile,
		CAFile:        config.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	// Set up a cluster with three nodes. The second and third nodes join the
	// first node's cluster.
	var agents []*agent.Agent
	for i := 0; i < 3; i++ {
		// Because we now have two addresses to configure in our service (the
		// gRPC address and the Serf address), and because we run our tests on a
		// single host, we need two ports. Now we don't want to get a port
		// automatically assigned to a listener by `net.Listen`. We just want
		// the port--with no listener--so we use the `freeport` package to
		// allocate the two ports we need: one for our gRPC log connections and
		// one for our Serf service discovery connections.
		ports := freeport.Get(2)
		bindAddr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
		rpcPort := ports[1]

		dataDir, err := ioutil.TempDir("", "agent_test_log")
		require.NoError(t, err)

		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.BindAddr)
		}

		agent, err := agent.New(agent.Config{
			NodeName:        fmt.Sprintf("%d", i),
			Bootstrap:       i == 0, // bootstrap the Raft cluster
			StartJoinAddrs:  startJoinAddrs,
			BindAddr:        bindAddr,
			RPCPort:         rpcPort,
			DataDir:         dataDir,
			ACLModelFile:    config.ACLModelFile,
			ACLPolicyFile:   config.ACLPolicyFile,
			ServerTLSConfig: serverTLSConfig,
			PeerTLSConfig:   peerTLSConfig,
		})
		require.NoError(t, err)

		agents = append(agents, agent)
	}
	// Defer a function call that runs after the test to verify that the agents
	// successfully shut down and to delete the test data.
	defer func() {
		for _, agent := range agents {
			err := agent.Shutdown()
			require.NoError(t, err)
			require.NoError(t, os.RemoveAll(agent.Config.DataDir))
		}
	}()
	// Make the test sleep for a few seconds to give the nodes time to discover
	// each other. In other words, wait until agents have joined the cluster.
	time.Sleep(3 * time.Second)

	// Now that we have a cluster, we can test it works.

	// Check that we can produce to and consume from a single node.
	leaderClient := client(t, agents[0], peerTLSConfig)
	produceResponse, err := leaderClient.Produce(
		context.Background(),
		&api.ProduceRequest{
			Record: &api.Record{
				Value: []byte("this is a test"),
			},
		},
	)
	require.NoError(t, err)
	consumeResponse, err := leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("this is a test"))

	// Now we need to check that another node replicated the record.

	// Because our replication works asynchronously across servers, the logs
	// produced to one server won't be immediately available on the replica
	// servers. This process causes latency between when the message is produced
	// to the first server and when it's replicated to the second. The stupid,
	// simple way to fix this (especially since we're black-box testing) is to
	// add a big enough delay in the test for the replicator to have replicated
	// the message, but as small a delay as possible to keep our tests fast.
	// Then we check that we can consume the replicated message.

	// Wait until replication has finished.
	time.Sleep(3 * time.Second)

	followerClient := client(t, agents[1], peerTLSConfig)
	consumeResponse, err = followerClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("this is a test"))

	// Verify that Raft has replicated the record we produced to the leader by
	// consuming the record from a follower and that the replication stops
	// there--the leader doesn't replicate from the followers. They must not
	// replicate each other in a cycle and must adhere to a leader-follower
	// relationship.
	consumeResponse, err = leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset + 1,
		},
	)
	require.Nil(t, consumeResponse)
	require.Error(t, err)
	got := status.Code(err)
	want := status.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err())
	require.Equal(t, got, want)
}

// client is a helper that sets up a client for the service.
func client(
	t *testing.T,
	agent *agent.Agent,
	tlsConfig *tls.Config,
) api.LogClient {
	tlsCreds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}

	rpcAddr, err := agent.Config.RPCAddr()
	require.NoError(t, err)

	conn, err := grpc.Dial(rpcAddr, opts...)
	require.NoError(t, err)

	client := api.NewLogClient(conn)

	return client
}
