package log_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"

	api "github.com/cedrickchee/commitlog/api/v1"
	"github.com/cedrickchee/commitlog/internal/log"
	"github.com/cedrickchee/commitlog/pkg/freeport"
)

// TestMultipleNodes sets up and tests a three-server cluster.
func TestMultipleNodes(t *testing.T) {
	var logs []*log.DistributedLog
	nodeCount := 3
	ports := freeport.Get(nodeCount)

	for i := 0; i < nodeCount; i++ {
		dataDir, err := ioutil.TempDir("", "distributed_log_test")
		require.NoError(t, err)
		defer func(dir string) {
			_ = os.RemoveAll(dir)
		}(dataDir)

		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]))
		require.NoError(t, err)

		config := log.Config{}
		config.Raft.StreamLayer = log.NewStreamLayer(ln, nil, nil)
		config.Raft.LocalID = raft.ServerID(fmt.Sprintf("%d", i))
		// Shorten the default Raft timeout configs so that Raft elects the
		// leader quickly.
		config.Raft.HeartbeatTimeout = 50 * time.Millisecond
		config.Raft.ElectionTimeout = 50 * time.Millisecond
		config.Raft.LeaderLeaseTimeout = 50 * time.Millisecond
		config.Raft.CommitTimeout = 5 * time.Millisecond

		// The first server bootstraps the cluster, becomes the leader, and adds
		// the other two servers to the cluster. The leader then must join other
		// servers to its cluster.

		if i == 0 {
			// Leader node
			config.Raft.Bootstrap = true
		}

		l, err := log.NewDistributedLog(dataDir, config)
		require.NoError(t, err)

		if i != 0 {
			// Add followers (voter nodes)

			// logs[0] is a leader node. Join operations must run on the leader.
			err = logs[0].Join(fmt.Sprintf("%d", i), ln.Addr().String())
			require.NoError(t, err)
		} else {
			// Leader node
			err = l.WaitForLeader(3 * time.Second)
			require.NoError(t, err)
		}

		logs = append(logs, l)
	}

	// Leader node
	leader := logs[0]

	// Test replication by appending some records to leader server and check
	// that Raft replicated the records to its followers. The Raft followers
	// will apply the append message after a short latency, so we use testify's
	// `Eventually()` method to give Raft enough time to finish replicating.

	records := []*api.Record{
		{Value: []byte("first")},
		{Value: []byte("second")},
	}
	for _, record := range records {
		offset, err := leader.Append(record)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			for j := 0; j < nodeCount; j++ {
				got, err := logs[j].Read(offset)
				if err != nil {
					return false
				}
				record.Offset = offset
				if !reflect.DeepEqual(got.Value, record.Value) {
					return false
				}
			}
			return true
		}, 500*time.Millisecond, 50*time.Millisecond)
	}

	// Check that the leader stops replicating to a server that's left the
	// cluster, while continuing to replicate to the existing servers.

	err := leader.Leave("1")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	want := []byte("third")
	offset, err := leader.Append(&api.Record{Value: want})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	record, err := logs[1].Read(offset)
	require.IsType(t, api.ErrOffsetOutOfRange{}, err)
	require.Nil(t, record)

	record, err = logs[2].Read(offset)
	require.NoError(t, err)
	require.Equal(t, want, record.Value)
	require.Equal(t, offset, record.Offset)
}
