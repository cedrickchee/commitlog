package agent

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	api "github.com/cedrickchee/commitlog/api/v1"
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

	log        *log.Log
	server     *grpc.Server
	membership *discovery.Membership
	replicator *log.Replicator

	shutdown     bool
	shutdowns    chan struct{}
	shutdownLock sync.Mutex
}

// Config comprises the components' parameters to pass them through to the
// components.
type Config struct {
	ServerTLSConfig *tls.Config
	PeerTLSConfig   *tls.Config
	DataDir         string
	// Serf (discovery) listens on this address and port for gossiping.
	// gRPC only listen on this address and get its port dynamically from the
	// `RPCPort` field.
	BindAddr string
	// gRPC port
	RPCPort        int
	NodeName       string
	StartJoinAddrs []string
	ACLModelFile   string
	ACLPolicyFile  string
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

	setup := []func() error{
		a.setupLogger,
		a.setupLog,
		a.setupServer,
		a.setupMembership,
	}
	for _, fn := range setup {
		if err := fn(); err != nil {
			return nil, err
		}
	}

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

// setupLog sets up the log.
func (a *Agent) setupLog() error {
	var err error
	a.log, err = log.NewLog(a.Config.DataDir, log.Config{})

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

	rpcAddr, err := a.RPCAddr()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}

	go func() {
		if err := a.server.Serve(ln); err != nil {
			_ = a.Shutdown()
		}
	}()

	return err
}

// setupMembership sets up the membership.
func (a *Agent) setupMembership() error {
	// Set up a `Replicator` with the gRPC dial options needed to connect to
	// other servers and a client so the `replicator` can connect to other
	// servers, consume their data, and produce a copy of the data to the
	// local server.

	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}

	var opts []grpc.DialOption
	if a.Config.PeerTLSConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(
			credentials.NewTLS(a.Config.PeerTLSConfig),
		),
		)
	}
	conn, err := grpc.Dial(rpcAddr, opts...)
	if err != nil {
		return err
	}

	client := api.NewLogClient(conn)

	// Then we create a `Membership` passing in the `replicator` and its handler
	// to notify the replicator when servers join and leave the cluster.

	a.replicator = &log.Replicator{
		DialOptions: opts,
		LocalServer: client,
	}
	tags := map[string]string{
		"rpc_addr": rpcAddr,
	}
	c := discovery.Config{
		NodeName:       a.Config.NodeName,
		BindAddr:       a.Config.BindAddr,
		Tags:           tags,
		StartJoinAddrs: a.Config.StartJoinAddrs,
	}
	a.membership, err = discovery.New(a.replicator, c)

	return err
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
		a.replicator.Close,
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
