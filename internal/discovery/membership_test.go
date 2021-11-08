package discovery_test

import (
	"fmt"
	"testing"
	"time"

	. "github.com/cedrickchee/commitlog/internal/discovery"
	"github.com/cedrickchee/commitlog/pkg/freeport"
	"github.com/hashicorp/serf/serf"
	"github.com/stretchr/testify/require"
)

// TestMembership sets up a cluster with multiple servers and checks that the
// `Membership` returns all the servers that joined the membership and updates
// after a server leaves the cluster.
func TestMembership(t *testing.T) {
	m, handler := setupMember(t, nil)
	m, _ = setupMember(t, m)
	m, _ = setupMember(t, m)

	// The handler's `join`s and `leave`s channels tell us how many times each
	// event happened and for what servers.
	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 &&
			len(m[0].Members()) == 3 &&
			len(handler.leaves) == 0
	}, 3*time.Second, 250*time.Millisecond)

	require.NoError(t, m[2].Leave())

	// Each member has a status to know how its doing:
	// - `Alive`: the server is present and healthy.
	// - `Left`: the server has gracefully left the cluster.
	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 &&
			len(m[0].Members()) == 3 &&
			serf.StatusLeft == m[0].Members()[2].Status &&
			len(handler.leaves) == 1
	}, 3*time.Second, 250*time.Millisecond)

	require.Equal(t, fmt.Sprintf("%d", 2), <-handler.leaves)
}

// setupMember is helper method to set up a member each time you call it.
func setupMember(t *testing.T, members []*Membership) ([]*Membership, *handler) {
	// Sets up a new member under a free port and with the member's length as
	// the node name so the names are unique. The member's length also tells us
	// whether this member is the cluster's initial member or we have a cluster
	// to join.
	id := len(members)
	ports := freeport.Get(1)
	addr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
	tags := map[string]string{
		"rpc_addr": addr,
	}
	c := Config{
		NodeName: fmt.Sprintf("%d", id),
		BindAddr: addr,
		Tags:     tags,
	}

	h := &handler{}

	if len(members) == 0 {
		h.joins = make(chan map[string]string, 3)
		h.leaves = make(chan string, 3)
	} else {
		c.StartJoinAddrs = []string{
			members[0].BindAddr,
		}
	}

	m, err := New(h, c)
	require.NoError(t, err)
	members = append(members, m)

	return members, h
}

// The handler mock tracks how many times our `Membership` calls the handler's
// `Join()` and `Leave()` methods, and with what IDs and addresses.
type handler struct {
	joins  chan map[string]string
	leaves chan string
}

func (h *handler) Join(id, addr string) error {
	if h.joins != nil {
		h.joins <- map[string]string{
			"id":   id,
			"addr": addr,
		}
	}
	return nil
}

func (h *handler) Leave(id string) error {
	if h.leaves != nil {
		h.leaves <- id
	}
	return nil
}
