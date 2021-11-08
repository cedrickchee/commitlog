package discovery

import (
	"net"

	"github.com/hashicorp/serf/serf"
	"go.uber.org/zap"
)

// Membership is a type wrapping Serf to provide discovery and cluster
// membership to a service.
type Membership struct {
	Config
	handler Handler
	// A Serf instance.
	serf *serf.Serf
	// The event channel is how we'll receive Serf's events when a node joins or
	// leaves the cluster.
	events chan serf.Event
	logger *zap.Logger
}

// New will be called to create a `Membership` with the required configuration
// and event handler.
func New(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config:  config,
		handler: handler,
		logger:  zap.L().Named("membership"),
	}
	if err := c.setupSerf(); err != nil {
		return nil, err
	}
	return c, nil
}

type Config struct {
	// Acts as the node's unique identifier across the Serf cluster. If you
	// don't set the node name, Serf uses the hostname.
	NodeName string
	// Serf listens on this address and port for gossiping.
	BindAddr string
	// Serf shares these tags to the other nodes in the cluster and should use
	// these tags for simple data that informs the cluster how to handle this
	// node. For example, similar to Consul, we'll share each node's
	// user-configured RPC address with this tag so the nodes know which
	// addresses to send their RPCs.
	Tags map[string]string
	// This field is how you configure new nodes to join an existing cluster. We
	// set the field to the addresses of nodes in the cluster, and Serf's gossip
	// protocol takes care of the rest to join your node to the cluster.
	StartJoinAddrs []string
}

// setupSerf creates and configures a Serf instance and starts the
// `eventsHandler()` goroutine to handle Serf's events.
func (m *Membership) setupSerf() (err error) {
	addr, err := net.ResolveTCPAddr("tcp", m.BindAddr)
	if err != nil {
		return err
	}
	config := serf.DefaultConfig()
	config.Init()
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port
	m.events = make(chan serf.Event)
	config.EventCh = m.events
	config.Tags = m.Tags
	config.NodeName = m.Config.NodeName
	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}
	go m.eventHandler()
	if m.StartJoinAddrs != nil {
		_, err = m.serf.Join(m.StartJoinAddrs, true)
		if err != nil {
			return err
		}
	}
	return nil
}

// Handler represents some component in our service that needs to know when a
// server joins or leaves the cluster. Example, a component that replicates the
// data of servers that join the cluster.
type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

// eventHandler runs in a loop reading events sent by Serf into the events
// channel, handling each incoming event according to the event's type.
func (m *Membership) eventHandler() {
	// When a node joins or leaves the cluster, Serf sends an event to all
	// nodes, including the node that joined or left the cluster.
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			// Notice that Serf may coalesce multiple members updates into one
			// event. For example, say ten nodes join around the same time; in
			// that case, Serf will send you one join event with ten members, so
			// that's why we iterate over the event's members.
			for _, member := range e.(serf.MemberEvent).Members {
				// Check whether the node we got an event for is the local
				// server so the server doesn't act on itself -- we don't want
				// the server to try and replicate itself, for example.
				if m.isLocal(member) {
					continue
				}
				m.handleJoin(member)
			}
		case serf.EventMemberLeave, serf.EventMemberFailed:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					return
				}
				m.handleLeave(member)
			}
		}
	}
}

func (m *Membership) handleJoin(member serf.Member) {
	if err := m.handler.Join(member.Name, member.Tags["rpc_addr"]); err != nil {
		m.logError(err, "failed to join", member)
	}
}

func (m *Membership) handleLeave(member serf.Member) {
	if err := m.handler.Leave(member.Name); err != nil {
		m.logError(err, "failed to leave", member)
	}
}

// isLocal returns whether the given Serf member is the local member by checking
// the members' names.
func (m *Membership) isLocal(member serf.Member) bool {
	return m.serf.LocalMember().Name == member.Name
}

// Members returns a point-in-time snapshot of the cluster's Serf members.
func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}

// Leave tells this member to leave the Serf cluster.
func (m *Membership) Leave() error {
	return m.serf.Leave()
}

// logError logs the given error and message.
func (m *Membership) logError(err error, msg string, member serf.Member) {
	m.logger.Error(
		msg,
		zap.Error(err),
		zap.String("name", member.Name),
		zap.String("rpc_addr", member.Tags["rpc_addr"]),
	)
}
