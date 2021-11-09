package log

import (
	"context"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	api "github.com/cedrickchee/commitlog/api/v1"
)

// Documentation
// =============
//
// Replicator component integrate discovery and membership with our service (log
// and server components) and replicate logs.
//
// We add replication in our service so that we store multiple copies of the log
// data when we have multiple servers in a cluster. Replication makes our
// service more resilient to failures.
//
// For now, our replication will be pull-based, with the replication component
// consuming from each discovered server and producing a copy to the local
// server.
//
// To add replication to our cluster, we need a replication component that acts
// as a membership handler handling when a server joins and leaves the cluster.
// When a server joins the cluster, the component will connect to the server and
// run a loop that consumes from the discovered server and produces to the local
// server.

// Replicator connects to other servers with the gRPC client.
//
// The replicator calls the produce function to save a copy of the messages it
// consumes from the other servers.
type Replicator struct {
	// Fields for configuring the gRPC client so it can authenticate with the
	// servers.
	// DialOptions field is how we pass in the options to configure the client.
	DialOptions []grpc.DialOption
	LocalServer api.LogClient

	logger *zap.Logger

	mu sync.Mutex
	// servers field is a map of server addresses to a channel, which the
	// replicator uses to stop replicating from a server when the server fails
	// or leaves the cluster.
	servers map[string]chan struct{}
	closed  bool
	close   chan struct{}
}

// Join adds the given server address to the list of servers to replicate and
// kicks off the add goroutine to run the actual replication logic.
func (r *Replicator) Join(name, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}

	if _, ok := r.servers[name]; ok {
		// Already replicating so skip.
		return nil
	}
	r.servers[name] = make(chan struct{})

	go r.replicate(addr, r.servers[name])

	return nil
}

func (r *Replicator) replicate(addr string, leave chan struct{}) {
	// Create a client and open up a stream to consume all logs on the server.
	cc, err := grpc.Dial(addr, r.DialOptions...)
	if err != nil {
		r.logError(err, "failed to dial", addr)
		return
	}
	defer cc.Close()

	client := api.NewLogClient(cc)

	ctx := context.Background()
	stream, err := client.ConsumeStream(ctx, &api.ConsumeRequest{Offset: 0})
	if err != nil {
		r.logError(err, "failed to consume", addr)
		return
	}

	records := make(chan *api.Record)
	go func() {
		for {
			recv, err := stream.Recv()
			if err != nil {
				r.logError(err, "failed to receive", addr)
				return
			}
			records <- recv.Record
		}
	}()

	// Consume the logs from the discovered server in a stream and then produce
	// to the local server to save a copy. We replicate messages from the other
	// server until that server fails or leaves the cluster and the replicator
	// closes the channel for that server, which breaks the loop and ends the
	// `replicate()` goroutine.
	for {
		select {
		case <-r.close:
			return
		case <-leave:
			// The replicator closes the channel when Serf receives an event
			// saying that the other server left the cluster, and then this
			// server calls the `Leave()` method.
			return
		case record := <-records:
			_, err := r.LocalServer.Produce(ctx,
				&api.ProduceRequest{Record: record},
			)
			if err != nil {
				r.logError(err, "failed to produce", addr)
				return
			}
		}
	}
}

// Leave handles the server leaving the cluster by removing the server from the
// list of servers to replicate and closes the server's associated channel.
// Closing the channel signals to the receiver in the `replicate()` goroutine to
// stop replicating from that server.
func (r *Replicator) Leave(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if _, ok := r.servers[name]; !ok {
		return nil
	}

	close(r.servers[name])
	delete(r.servers, name)

	return nil
}

// init is a helper to lazily initialize the server map. You should use lazy
// initialization to give your structs a useful zero value because having a
// useful zero value reduces the API's size and complexity while maintaining the
// same functionality. Without a useful zero value, we'd either have to export a
// replicator constructor function for the user to call or export the servers
// field on the replicator struct for the user to set—making more API for the
// user to learn and then requiring them to write more code before they can use
// our struct.
func (r *Replicator) init() {
	if r.logger == nil {
		r.logger = zap.L().Named("replicator")
	}
	if r.servers == nil {
		r.servers = make(map[string]chan struct{})
	}
	if r.close == nil {
		r.close = make(chan struct{})
	}
}

// Close closes the replicator so it doesn't replicate new servers that join the
// cluster and it stops replicating existing servers by causing the
// `replicate()` goroutines to return.
func (r *Replicator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}

	r.closed = true
	close(r.close)

	return nil
}

// logError is a helper to handle errors. We just log the errors because we have
// no other use for them and to keep the code short and simple.
func (r *Replicator) logError(err error, msg, addr string) {
	r.logger.Error(
		msg,
		zap.String("addr", addr),
		zap.Error(err),
	)
}
