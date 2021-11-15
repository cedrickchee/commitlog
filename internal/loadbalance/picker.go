package loadbalance

// In the gRPC architecture, pickers handle the RPC balancing logic. They're
// called pickers because they pick a server from the servers discovered by the
// resolver to handle each RPC. Pickers can route RPCs based on information
// about the RPC, client, and server, so their utility goes beyond balancing to
// any kind of request-routing logic.

import (
	"strings"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

type Picker struct {
	mu        sync.RWMutex
	leader    balancer.SubConn
	followers []balancer.SubConn
	current   uint64
}

// Build creates a picker.
//
// Picker use the builder pattern just like resolvers. gRPC passes a map of
// subconnections with information about those subconnections to `Build()` to
// set up the picker-- behind the scenes, gRPC connected to the addresses that
// our resolver discovered. Our picker will route consume RPCs to follower
// servers and produce RPCs to the leader server. The address attributes help us
// differentiate the servers.
func (p *Picker) Build(buildInfo base.PickerBuildInfo) balancer.Picker {
	p.mu.Lock()
	defer p.mu.Unlock()

	var followers []balancer.SubConn

	// Range over a map of subconnections with info about those subconnections.
	for sc, scInfo := range buildInfo.ReadySCs {
		isLeader := scInfo.Address.Attributes.Value("is_leader").(bool)
		if isLeader {
			p.leader = sc
			continue
		}
		followers = append(followers, sc)
	}
	p.followers = followers

	return p
}

// Implement the picker.

var _ balancer.Picker = (*Picker)(nil)

// Pick returns the connection to use for this RPC and related information.
//
// gRPC gives `Pick()` a `balancer.PickInfo` containing the RPC's name and
// context to help the picker know what subconnection to pick. If you have
// header metadata, you can read it from the context. `Pick()` returns a
// `balancer.PickResult` with the subconnection to handle the call.
func (p *Picker) Pick(info balancer.PickInfo) (
	balancer.PickResult,
	error,
) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result balancer.PickResult
	// Look at the RPC's method name to know whether the call is an produce or
	// consume call, and if we should pick a leader subconnection or a follower
	// subconnection.
	if strings.Contains(info.FullMethodName, "Produce") ||
		len(p.followers) == 0 {
		result.SubConn = p.leader
	} else if strings.Contains(info.FullMethodName, "Consume") {
		result.SubConn = p.nextFollower()
	}
	if result.SubConn == nil {
		// `balancer.ErrNoSubConnAvailable` instructs gRPC to block the client's
		// RPCs until the picker has an available subconnection to handle them.
		return result, balancer.ErrNoSubConnAvailable
	}

	return result, nil
}

// nextFollower balances the consume RPC calls across the followers with the
// round-robin algorithm.
func (p *Picker) nextFollower() balancer.SubConn {
	cur := atomic.AddUint64(&p.current, uint64(1))
	len := uint64(len(p.followers))
	idx := int(cur % len)

	return p.followers[idx]
}

// init register the picker with gRPC.
func init() {
	// Though pickers handle routing the calls, which we'd traditionally
	// consider handling the balancing, gRPC has a balancer type that takes
	// input from gRPC, manages subconnections, and collects and aggregates
	// connectivity states. gRPC provides a base balancer; you probably don't
	// need to write your own.
	balancer.Register(base.NewBalancerBuilder(Name, &Picker{}, base.Config{}))
}
