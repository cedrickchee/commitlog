package agent

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/cedrickchee/commitlog/internal/auth"
	"github.com/cedrickchee/commitlog/internal/discovery"
	"github.com/cedrickchee/commitlog/internal/log"
	"github.com/cedrickchee/commitlog/internal/server"
)

// An Agent runs on every service instance, setting up and connecting all the
// different components.

// Agent struct references each component (log, server, membership, replicator)
// that the Agent manages.
type Agent struct {
	// The agent config.
	Config

	mux        cmux.CMux
	log        *log.DistributedLog
	server     *grpc.Server
	membership *discovery.Membership

	shutdown     bool
	shutdowns    chan struct{}
	shutdownLock sync.Mutex
}

// Config comprises the components' parameters to pass them through to the
// components.
type Config struct {
	ServerTLSConfig *tls.Config
	PeerTLSConfig   *tls.Config
	// DataDir stores the log and raft data.
	DataDir string
	// The address Serf (discovery & membership) runs on.
	// Serf listens on this address and port for gossiping.
	// gRPC only listen on this address and get its port dynamically from the
	// `RPCPort` field.
	BindAddr string
	// The port for gRPC client (and Raft) connections.
	RPCPort int
	// Raft server id.
	NodeName       string
	StartJoinAddrs []string
	ACLModelFile   string
	ACLPolicyFile  string
	// Bootstrap should be set to true when starting the first node of the
	// cluster.
	Bootstrap bool
}

// RPCAddr returns the address and port for gRPC server.
func (c Config) RPCAddr() (string, error) {
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}

// New creates an Agent and runs a set of methods to set up and run the agent's
// components. After we run `New()`, we expect to have a running, functioning
// service.
func New(config Config) (*Agent, error) {
	a := &Agent{
		Config:    config,
		shutdowns: make(chan struct{}),
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	zap.ReplaceGlobals(logger)

	setup := []func() error{
		a.setupLogger,
		a.setupMux,
		a.setupLog,
		a.setupServer,
		a.setupMembership,
	}
	for _, fn := range setup {
		if err := fn(); err != nil {
			return nil, err
		}
	}

	go a.serve()

	return a, nil
}

// setupLogger sets up the logger.
func (a *Agent) setupLogger() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	zap.ReplaceGlobals(logger)

	return nil
}

// setupMux creates a listener on our RPC address that'll accept both Raft and
// gRPC connections and then creates the mux (short for multiplexer) with the
// listener. The mux will accept connections on that listener and match
// connections based on your configured rules.
func (a *Agent) setupMux() error {
	rpcAddr := fmt.Sprintf(":%d", a.Config.RPCPort)
	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}
	a.mux = cmux.New(ln)

	return nil
}

// setupLog sets up the log.
func (a *Agent) setupLog() error {
	// Configure the mux rule that matches Raft connections.
	raftLn := a.mux.Match(func(reader io.Reader) bool {
		// Identify Raft connections by reading one byte and checking that the
		// byte matches the byte we set up our outgoing Raft connections to
		// write in Stream Layer.
		b := make([]byte, 1)
		if _, err := reader.Read(b); err != nil {
			return false
		}
		// If the mux matches this rule, it will pass the connection to the
		// `raftLn` listener for Raft to handle the connection.
		return bytes.Equal(b, []byte{byte(log.RaftRPC)})
	})

	// Configure the distributed log's Raft to use our multiplexed listener and
	// then configure and create the distributed log.
	logConfig := log.Config{}
	logConfig.Raft.StreamLayer = log.NewStreamLayer(
		raftLn,
		a.Config.ServerTLSConfig,
		a.Config.PeerTLSConfig,
	)
	logConfig.Raft.LocalID = raft.ServerID(a.Config.NodeName)
	logConfig.Raft.Bootstrap = a.Config.Bootstrap

	var err error
	a.log, err = log.NewDistributedLog(a.Config.DataDir, logConfig)
	if err != nil {
		return err
	}
	if a.Config.Bootstrap {
		err = a.log.WaitForLeader(3 * time.Second)
	}

	return err
}

// setupServer sets up the server.
func (a *Agent) setupServer() error {
	authorizer, err := auth.New(a.Config.ACLModelFile, a.Config.ACLPolicyFile)
	if err != nil {
		return err
	}

	serverConfig := &server.Config{
		CommitLog:  a.log,
		Authorizer: authorizer,
		// Configure the server to get the cluster's servers from the
		// `DistributedLog`.
		GetServerer: a.log,
	}

	var opts []grpc.ServerOption
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}

	a.server, err = server.NewGRPCServer(serverConfig, opts...)
	if err != nil {
		return err
	}

	// Because we've multiplexed two connection types (Raft and gRPC) and we
	// added a matcher for the Raft connections, we know all other connections
	// must be gRPC connections.
	grpcLn := a.mux.Match(cmux.Any())

	go func() {
		// Tell the gRPC server to serve on the multiplexed listener.
		if err := a.server.Serve(grpcLn); err != nil {
			_ = a.Shutdown()
		}
	}()

	return err
}

// setupMembership sets up the membership.
func (a *Agent) setupMembership() error {
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}

	// Our `DistributedLog` handles coordinated replication, thanks to Raft, so
	// we don’t need the `Replicator` anymore. Now we need the `Membership` to
	// tell the `DistributedLog` when servers join or leave the cluster.

	tags := map[string]string{
		"rpc_addr": rpcAddr,
	}
	c := discovery.Config{
		NodeName:       a.Config.NodeName,
		BindAddr:       a.Config.BindAddr,
		Tags:           tags,
		StartJoinAddrs: a.Config.StartJoinAddrs,
	}
	// Our distributed log will act as our `Membership`'s handler.
	// Create a `Membership` passing in the distributed log and its handler to
	// notify Raft when servers join and leave the cluster.
	a.membership, err = discovery.New(a.log, c)

	return err
}

// serve tells our mux to serve connections.
func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		_ = a.Shutdown()
		return err
	}

	return nil
}

// Shutdown shuts down the agent. This ensures that the agent will shut down
// once even if people call `Shutdown()` multiple times.
func (a *Agent) Shutdown() error {
	// Shut down the agent and its components by:
	// - Leaving the membership so that other servers will see that this server
	//   has left the cluster and so that this server doesn't receive discovery
	//   events anymore;
	// - Closing the replicator so it doesn't continue to replicate;
	// - Gracefully stopping the server, which stops the server from accepting
	//   new connections and blocks until all the pending RPCs have
	//   finished; and
	// - Closing the log.
	a.shutdownLock.Lock()
	defer a.shutdownLock.Unlock()

	if a.shutdown {
		return nil
	}
	a.shutdown = true
	close(a.shutdowns)

	shutdown := []func() error{
		a.membership.Leave,
		func() error {
			a.server.GracefulStop()
			return nil
		},
		a.log.Close,
	}
	for _, fn := range shutdown {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}
