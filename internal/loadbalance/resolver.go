package loadbalance

// Resolver resolves the servers.
//
// The gRPC resolver will call the `GetServers()` endpoint and pass its
// information to gRPC so that the picker (in load balancer) knows what servers
// it can route requests to.

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	api "github.com/cedrickchee/commitlog/api/v1"
)

// Resolver is the type we'll implement into gRPC's `resolver.Builder` and
// `resolver.Resolver` interfaces.
type Resolver struct {
	mu sync.Mutex
	// The user's client connection. gRPC passes it to the resolver for the
	// resolver to update with the servers it discovers.
	clientConn resolver.ClientConn
	// The resolver's own client connection to the server so it can call
	// `GetServers()` and get the servers.
	resolverConn *grpc.ClientConn
	// Services can specify how clients should balance their calls to the
	// service by updating the state with a service config.
	serviceConfig *serviceconfig.ParseResult
	logger        *zap.Logger
}

// gRPC uses the builder pattern for resolvers and pickers, so each has a
// builder interface and an implementation interface.

// Implement gRPC's `resolver.Builder` interface.

var _ resolver.Builder = (*Resolver)(nil)

// Build receives the data needed to build a resolver that can discover the
// servers (like the target address) and the client connection the resolver will
// update with the servers it discovers.
func (r *Resolver) Build(
	target resolver.Target,
	cc resolver.ClientConn,
	opts resolver.BuildOptions,
) (resolver.Resolver, error) {
	r.logger = zap.L().Named("resolver")

	// Set up a client connection to our server so the resolver can call the
	// `GetServers()` API.

	r.clientConn = cc

	var dialOpts []grpc.DialOption
	if opts.DialCreds != nil {
		dialOpts = append(
			dialOpts,
			grpc.WithTransportCredentials(opts.DialCreds),
		)
	}

	r.serviceConfig = r.clientConn.ParseServiceConfig(
		fmt.Sprintf(`{"loadBalancingConfig":[{"%s":{}}]}`, Name),
	)

	var err error
	r.resolverConn, err = grpc.Dial(target.Endpoint, dialOpts...)
	if err != nil {
		return nil, err
	}

	r.ResolveNow(resolver.ResolveNowOptions{})

	return r, nil
}

var Name = "commitlog"

// Scheme returns the resolver's scheme identifier. When you call `grpc.Dial`,
// gRPC parses out the scheme from the target address you gave it and tries to
// find a resolver that matches, defaulting to its DNS resolver. For our
// resolver, you'll format the target address like this:
// `commitlog://your-service-address`.
func (r *Resolver) Scheme() string {
	return Name
}

// init register this resolver with gRPC so gRPC knows about this resolver when
// it's looking for resolvers that match the target's scheme.
func init() {
	resolver.Register(&Resolver{})
}

// Implement gRPC's `resolver.Resolver` interface.

var _ resolver.Resolver = (*Resolver)(nil)

// ResolveNow will be called by gRPC to resolve the target, discover the
// servers, and update the client connection with the servers.
func (r *Resolver) ResolveNow(resolver.ResolveNowOptions) {
	// gRPC may call `ResolveNow()` concurrently, so we use a mutex to protect
	// access across goroutines.
	r.mu.Lock()
	defer r.mu.Unlock()

	// How your resolver will discover the servers depends on your resolver and
	// the service you're working with. For example, a resolver built for
	// Kubernetes could call Kubernetes' API to get the list of endpoints.
	// We create a gRPC client for our service and call the `GetServers()` API
	// to get the cluster's servers.

	client := api.NewLogClient(r.resolverConn)
	ctx := context.Background()
	res, err := client.GetServers(ctx, &api.GetServersRequest{})
	if err != nil {
		r.logger.Error("failed to resolve server", zap.Error(err))
		return
	}

	// We update the state with a slice of `resolver.Address` to inform the load
	// balancer what servers it can choose from.
	var addrs []resolver.Address
	for _, server := range res.Servers {
		addrs = append(addrs, resolver.Address{
			// The address of the server to connect to.
			Addr: server.RpcAddr,
			// A map containing any data that's useful for the load balancer.
			// We'll tell the picker what server is the leader and what servers
			// are followers with this field.
			Attributes: attributes.New("is_leader", server.IsLeader),
		})
	}

	// After we’ve discovered the servers, we update the client connection by
	// calling `UpdateState()` with the `resolver.Address`'s. We set up the
	// addresses with the data in the api.Server’s.
	r.clientConn.UpdateState(resolver.State{
		// Addresses is the latest set of resolved addresses for the target.
		Addresses: addrs,
		// Update the state with a service config that specifies to use the
		// "commitlog" load balancer.
		ServiceConfig: r.serviceConfig,
	})
}

// Close closes the resolver.
func (r *Resolver) Close() {
	// In our resolver, we close the connection to our server created in
	// `Build()`.
	if err := r.resolverConn.Close(); err != nil {
		r.logger.Error("failed to close conn", zap.Error(err))
	}
}
