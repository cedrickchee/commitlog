package log

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	"google.golang.org/protobuf/proto"

	api "github.com/cedrickchee/commitlog/api/v1"
)

// DistributedLog implements a replicated log built with Raft.
type DistributedLog struct {
	// log's configuration
	config Config
	// log is the single-server, non-replicated log
	log  *Log
	raft *raft.Raft
}

// NewDistributedLog is a factory function to create the log.
func NewDistributedLog(dataDir string, config Config) (*DistributedLog, error) {
	l := &DistributedLog{
		config: config,
	}

	// Defers the logic to the setup methods.
	if err := l.setupLog(dataDir); err != nil {
		return nil, err
	}
	if err := l.setupRaft(dataDir); err != nil {
		return nil, err
	}

	return l, nil
}

// setupLog creates the log for this server, where this server will store the
// user's records.
func (l *DistributedLog) setupLog(dataDir string) error {
	logDir := filepath.Join(dataDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	var err error
	l.log, err = NewLog(logDir, l.config)

	return err
}

// setupRaft configures and creates the server's Raft instance.
// A Raft instance comprises:
// - A finite-state machine that applies the commands you give Raft;
// - A log store where Raft stores those commands;
// - A stable store where Raft stores the cluster's configuration—the servers in
//   the cluster, their addresses, and so on;
// - A snapshot store where Raft stores compact snapshots of its data; and
// - A transport that Raft uses to connect with the server's peers.
func (l *DistributedLog) setupRaft(dataDir string) error {
	// Create a finite-state machine (FSM).
	fsm := &fsm{log: l.log}

	// Create Raft's log store, and we use our own log we wrote.

	logDir := filepath.Join(dataDir, "raft", "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logConfig := l.config
	// Configure our log's initial offset to `1`, as required by Raft.
	logConfig.Segment.InitialOffset = 1
	// Raft needs a specific log interface satisfied, so we'll wrap our log to
	// provide those APIs.
	logStore, err := newLogStore(logDir, logConfig)
	if err != nil {
		return err
	}

	// The stable store is a key-value store where Raft stores important
	// metadata, like the server's current term or the candidate the server
	// voted for. Bolt is an embedded and persisted key-value database for Go
	// we've used as our stable store.
	stableStore, err := raftboltdb.NewBoltStore(
		filepath.Join(dataDir, "raft", "stable"))
	if err != nil {
		return err
	}

	// Set up Raft's snapshot store. Raft snapshots to recover and restore data
	// efficiently, when necessary, like if your server's EC2 instance failed
	// and an autoscaling group brought up another instance for the Raft server.
	// Rather than streaming all the data from the Raft leader, the new server
	// would restore from the snapshot (which you could store in S3 or a similar
	// storage service) and then get the latest changes from the leader. This is
	// more efficient and less taxing on the leader. You want to snapshot
	// frequently to minimize the difference between the data in the snapshots
	// and on the leader. The retain variable specifies that we'll keep one
	// snapshot.
	retain := 1
	snapshotStore, err := raft.NewFileSnapshotStore(
		filepath.Join(dataDir, "raft"),
		retain,
		os.Stderr,
	)
	if err != nil {
		return err
	}

	// Create our transport that wraps a stream layer--a low-level stream
	// abstraction.
	maxPool := 5
	timeout := 10 * time.Second
	transport := raft.NewNetworkTransport(
		l.config.Raft.StreamLayer,
		maxPool,
		timeout,
		os.Stderr,
	)

	// `LocalID` field is the unique ID for this server, and it's the only
	// config field we must set; the rest are optional, and in normal operation
	// the default config should be fine.
	config := raft.DefaultConfig()
	config.LocalID = l.config.Raft.LocalID
	// To make our tests faster, we support overriding a handful of timeout
	// configs to speed up Raft.
	if l.config.Raft.HeartbeatTimeout != 0 {
		config.HeartbeatTimeout = l.config.Raft.HeartbeatTimeout
	}
	if l.config.Raft.ElectionTimeout != 0 {
		config.ElectionTimeout = l.config.Raft.ElectionTimeout
	}
	if l.config.Raft.LeaderLeaseTimeout != 0 {
		config.LeaderLeaseTimeout = l.config.Raft.LeaderLeaseTimeout
	}
	if l.config.Raft.CommitTimeout != 0 {
		config.CommitTimeout = l.config.Raft.CommitTimeout
	}

	// Create the Raft instance and bootstrap the cluster.
	l.raft, err = raft.NewRaft(
		config,
		fsm,
		logStore,
		stableStore,
		snapshotStore,
		transport,
	)
	if err != nil {
		return err
	}
	hasState, err := raft.HasExistingState(
		logStore,
		stableStore,
		snapshotStore,
	)
	if err != nil {
		return err
	}
	// Generally we'll bootstrap a server configured with itself as the only
	// voter, wait until it becomes the leader, and then tell the leader to add
	// more servers to the cluster. The subsequently added servers don't
	// bootstrap.
	if l.config.Raft.Bootstrap && !hasState {
		config := raft.Configuration{
			Servers: []raft.Server{{
				ID: config.LocalID,
				// We want to use the Fully Qualified Domain Name (FQDN) instead
				// so the Raft node will properly advertise itself to its
				// cluster and to its clients. To achieve that, we use the
				// configured bind address instead of the transport's local
				// address.
				Address: raft.ServerAddress(l.config.Raft.BindAddr),
			}},
		}
		err = l.raft.BootstrapCluster(config).Error()
	}

	return err
}

// Append appends a record to the log.
func (l *DistributedLog) Append(record *api.Record) (uint64, error) {
	res, err := l.apply(AppendRequestType, &api.ProduceRequest{Record: record})
	if err != nil {
		return 0, err
	}

	return res.(*api.ProduceResponse).Offset, nil
}

// apply wraps Raft's API to apply requests and return their responses.
func (l *DistributedLog) apply(reqType RequestType, req proto.Message) (
	interface{},
	error,
) {
	// Unlike `Log`, where we append the record directly to this server's log,
	// `DistributedLog` tells Raft to apply a command (we've reused for the
	// `ProduceRequest` for the command) that tells the FSM to append the record
	// to the log.
	//
	// Raft runs the log replication process to replicate the command to a
	// majority of the Raft servers and ultimately append the record to a
	// majority of Raft servers.

	// First, marshal the request type and request into bytes that Raft uses as
	// the record's data it replicates.
	var buf bytes.Buffer
	_, err := buf.Write([]byte{byte(reqType)})
	if err != nil {
		return nil, err
	}
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(b)
	if err != nil {
		return nil, err
	}
	timeout := 10 * time.Second
	// This call has a lot going on behind the scenes, running the log
	// replication steps to replicate the record and append the record to the
	// leader's log.
	future := l.raft.Apply(buf.Bytes(), timeout)
	// The `future.Error()` API returns an error when something went wrong with
	// Raft's replication. For example, it took too long for Raft to process the
	// command or the server had to shutdown--the `future.Error()` API doesn't
	// return your service's errors. The `future.Response()` API returns what
	// your FSM's `Apply()` method returned and, opposed to Go's convention of
	// using Go's multiple return values to separate errors, you must return a
	// single value for Raft. In our `apply()` method we check whether the value
	// is an error with a type assertion.
	if future.Error() != nil {
		return nil, future.Error()
	}
	res := future.Response()
	if err, ok := res.(error); ok {
		return nil, err
	}

	return res, nil
}

// Read reads the record for the offset from the server's log. When we're okay
// with relaxed consistency, read operations need not go through Raft. When you
// need strong consistency, where reads must be up-to-date with writes, then you
// must go through Raft, but then reads are less efficient and take longer.
func (l *DistributedLog) Read(offset uint64) (*api.Record, error) {
	return l.log.Read(offset)
}

// *****************************************************************************
// Discovery integration
// *****************************************************************************

// Integrate our Serf-driven discovery layer with Raft to make the corresponding
// change in our Raft cluster when the Serf membership changes. Each time you
// add a server to the cluster, Serf will publish an event saying a member
// joined, and our `discovery.Membership` will call its handler’s `Join()`
// method. When a server leaves the cluster, Serf will publish an event saying a
// member left, and our `discovery.Membership` will call its handler’s `Leave()`
// method. Our distributed log will act as our `Membership`'s handler, so we
// need to implement those `Join()` and `Leave()` methods to update Raft.

// Join adds the server to the Raft cluster. We add every server as a voter, but
// Raft supports adding servers as non-voters with the `AddNonVoter()` API.
func (l *DistributedLog) Join(id, addr string) error {
	configFuture := l.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return err
	}
	serverID := raft.ServerID(id)
	serverAddr := raft.ServerAddress(addr)
	for _, srv := range configFuture.Configuration().Servers {
		if srv.ID == serverID || srv.Address == serverAddr {
			if srv.ID == serverID && srv.Address == serverAddr {
				// Server has already joined.
				return nil
			}
			// Remove the existing server.
			removeFuture := l.raft.RemoveServer(serverID, 0, 0)
			if err := removeFuture.Error(); err != nil {
				return err
			}
		}
	}
	addFuture := l.raft.AddVoter(serverID, serverAddr, 0, 0)
	if err := addFuture.Error(); err != nil {
		return err
	}

	return nil
}

// Leave removes the server from the cluster. Removing the leader will trigger a
// new election.
func (l *DistributedLog) Leave(id string) error {
	removeFuture := l.raft.RemoveServer(raft.ServerID(id), 0, 0)
	return removeFuture.Error()
}

// WaitForLeader blocks until the cluster has elected a leader or times out.
// It's useful when writing tests because most operations must run on the
// leader.
func (l *DistributedLog) WaitForLeader(timeout time.Duration) error {
	timeoutc := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeoutc:
			return fmt.Errorf("timed out")
		case <-ticker.C:
			if l := l.raft.Leader(); l != "" {
				return nil
			}
		}
	}
}

// Close shuts down the Raft instance and closes the local log.
func (l *DistributedLog) Close() error {
	shutdownFuture := l.raft.Shutdown()
	if err := shutdownFuture.Error(); err != nil {
		return err
	}

	return l.log.Close()
}

// GetServers exposes Raft's server data -- converts the data from Raft's
// `raft.Server` type into our `*api.Server` type for our API to respond with.
func (l *DistributedLog) GetServers() ([]*api.Server, error) {
	// Raft's configuration comprises the servers in the cluster and includes
	// each server's ID, address, and suffrage—whether the server votes in Raft
	// elections (we don't need the suffrage in our project). Raft can tell us
	// the address of the cluster's leader, too.
	future := l.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil, err
	}
	var servers []*api.Server
	for _, server := range future.Configuration().Servers {
		servers = append(servers, &api.Server{
			Id:       string(server.ID),
			RpcAddr:  string(server.Address),
			IsLeader: l.raft.Leader() == server.Address,
		})
	}

	return servers, nil
}

// *****************************************************************************
// Finite-State Machine (FSM)
// *****************************************************************************

// Raft defers the running of our business logic to the FSM.
//
// The FSM must access the data it manages. In our service, that's a log, and
// the FSM appends records to the log. If we were writing a key-value service,
// then our FSM would update the store of our data: an int, a map,
// Postgres--whatever store you've used.

// Assign value of type *fsm to an interface type
var _ raft.FSM = (*fsm)(nil) // type conversion syntax

type fsm struct {
	log *Log
}

// Even though our service has only one command to replicate, we want to develop
// things to support multiple commands.

// RequestType is our own request type.
type RequestType uint8

// These request types identify the request and tell us how to handle it.
const (
	AppendRequestType RequestType = 0
)

// Your FSM must implement three methods:

// Apply will be invoked by Raft after committing a log entry.
func (f *fsm) Apply(record *raft.Log) interface{} {
	// Read the request we sent to Raft.
	buf := record.Data
	// Identify the request type and then call the corresponding method
	// containing the logic to run the command.
	reqType := RequestType(buf[0])
	switch reqType {
	case AppendRequestType:
		return f.applyAppend(buf[1:])
	}
	return nil
}

func (f *fsm) applyAppend(b []byte) interface{} {
	// Unmarshal the request and then append the record to the local log and
	// return the response for Raft to send back to where we called
	// `raft.Apply()` in `DistributedLog.Append()`.
	var req api.ProduceRequest
	err := proto.Unmarshal(b, &req)
	if err != nil {
		return err
	}
	offset, err := f.log.Append(req.Record)
	if err != nil {
		return err
	}
	return &api.ProduceResponse{Offset: offset}
}

// Snapshot is used to support log compaction. Raft periodically calls this
// method to snapshot its state according to your configured `SnapshotInterval`
// and `SnapshotThreshold`. It returns an `FSMSnapshot` that represents a
// point-in-time snapshot of the FSM's state. In our case that state is our
// FSM's log.
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	// Returns an `io.Reader` that will read all the log's data.
	r := f.log.Reader()
	return &snapshot{reader: r}, nil
}

var _ raft.FSMSnapshot = (*snapshot)(nil)

type snapshot struct {
	reader io.Reader
}

// Persist will write the state (whole local log) to some sink that, depending
// on the snapshot store you configured Raft with, could be in-memory, a file,
// an S3 bucket—something to store the bytes in. In our case that snapshot store
// is the `FileSnapshotStore` so that when the snapshot completes, we'll have a
// file containing all the Raft's log data.
func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	_, err := io.Copy(sink, s.reader)
	if err != nil {
		// On error, call Cancel.
		_ = sink.Cancel()
		return err
	}
	// On completion, call Close.
	return sink.Close()
}

// Release is called when Raft is finished with the snapshot.
func (s *snapshot) Release() {}

// Restore is used by Raft to restore an FSM from a snapshot. For example, if we
// lost a server and scaled up a new one, we'd want to restore its FSM. The FSM
// must discard existing state to make sure its state will match the leader's
// replicated state.
func (f *fsm) Restore(r io.ReadCloser) error {
	b := make([]byte, lenWidth)
	var buf bytes.Buffer
	for i := 0; ; i++ {
		_, err := io.ReadFull(r, b)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		size := int64(enc.Uint64(b))
		if _, err = io.CopyN(&buf, r, size); err != nil {
			return err
		}
		record := &api.Record{}
		if err = proto.Unmarshal(buf.Bytes(), record); err != nil {
			return err
		}

		// Reset the log and configure its initial offset to the first record's
		// offset we read from the snapshot so the log's offsets match. Then we
		// read the records in the snapshot and append them to our new log.
		if i == 0 {
			f.log.Config.Segment.InitialOffset = record.Offset
			if err := f.log.Reset(); err != nil {
				return err
			}
		}
		if _, err = f.log.Append(record); err != nil {
			return err
		}
		buf.Reset()
	}
	return nil
}

// *****************************************************************************
// Raft's durable log store
// *****************************************************************************

// Raft calls our FSM's `Apply()` method with `*raft.Log`'s read from its
// managed log store. Raft replicates a log and then calls your state machine
// with the log's records.

var _ raft.LogStore = (*logStore)(nil)

// logStore wraps our log to satisfy the `LogStore` interface Raft requires.
type logStore struct {
	*Log
}

// newLogStore creates our own log as Raft's log store.
func newLogStore(dir string, c Config) (*logStore, error) {
	log, err := NewLog(dir, c)
	if err != nil {
		return nil, err
	}
	return &logStore{log}, nil
}

// Raft uses these APIs to get records and info about the log. We support the
// functionality on our log already and just needed to wrap our existing
// methods. What we call offsets, Raft calls indexes. We just translate the call
// to our log's API and our record type.

// FirstIndex returns the first index written.
func (l *logStore) FirstIndex() (uint64, error) {
	return l.LowestOffset()
}

// LastIndex returns the last index written.
func (l *logStore) LastIndex() (uint64, error) {
	return l.HighestOffset()
}

// GetLog gets a log entry at a given index.
func (l *logStore) GetLog(index uint64, out *raft.Log) error {
	in, err := l.Read(index)
	if err != nil {
		return err
	}
	out.Data = in.Value
	out.Index = in.Offset
	out.Type = raft.LogType(in.Type)
	out.Term = in.Term
	return nil
}

// StoreLog stores a log entry.
func (l *logStore) StoreLog(record *raft.Log) error {
	return l.StoreLogs([]*raft.Log{record})
}

// StoreLogs stores multiple log entries.
func (l *logStore) StoreLogs(records []*raft.Log) error {
	for _, record := range records {
		if _, err := l.Append(&api.Record{
			Value: record.Data,
			Term:  record.Term,
			Type:  uint32(record.Type),
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRange deletes a range of records that are old or stored in a snapshot.
func (l *logStore) DeleteRange(min, max uint64) error {
	return l.Truncate(max)
}

// *****************************************************************************
// Stream layer
// *****************************************************************************

// Raft uses a stream layer in the transport to provide a low-level stream
// abstraction to connect with Raft servers. Our stream layer must satisfy
// Raft's `StreamLayer` interface.

// Check that `StreamLayer` type satisfies the `raft.StreamLayer` interface.
var _ raft.StreamLayer = (*StreamLayer)(nil)

// StreamLayer is used with the `raft.NetworkTransport`.
type StreamLayer struct {
	ln              net.Listener
	serverTLSConfig *tls.Config
	peerTLSConfig   *tls.Config
}

// NewStreamLayer creates a `StreamLayer`. We want to enable encrypted
// communication between servers with TLS, so we need to take in the TLS configs
// used to accept incoming connections (the `serverTLSConfig`) and create
// outgoing connections (the `peerTLSConfig`).
func NewStreamLayer(
	ln net.Listener,
	serverTLSConfig,
	peerTLSConfig *tls.Config,
) *StreamLayer {
	return &StreamLayer{
		ln:              ln,
		serverTLSConfig: serverTLSConfig,
		peerTLSConfig:   peerTLSConfig,
	}
}

const RaftRPC = 1

// Dial is used to make outgoing connections to other servers in the Raft
// cluster. When we connect to a server, we write the `RaftRPC` byte to identify
// the connection type so we can multiplex Raft on the same port as our Log gRPC
// requests.
func (s *StreamLayer) Dial(
	addr raft.ServerAddress,
	timeout time.Duration,
) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	var conn, err = dialer.Dial("tcp", string(addr))
	if err != nil {
		return nil, err
	}
	// Identify to mux this is a raft rpc.
	_, err = conn.Write([]byte{byte(RaftRPC)})
	if err != nil {
		return nil, err
	}
	// If we configure the stream layer with a peer TLS config, we make a TLS
	// client-side connection.
	if s.peerTLSConfig != nil {
		conn = tls.Client(conn, s.peerTLSConfig)
	}

	return conn, err
}

// The rest of the methods on the stream layer implement the `net.Listener`
// interface.

// Accept is the mirror of Dial. We wait and accept the incoming connection and
// read the byte that identifies the connection and then create a server-side
// TLS connection.
func (s *StreamLayer) Accept() (net.Conn, error) {
	conn, err := s.ln.Accept()
	if err != nil {
		return nil, err
	}
	b := make([]byte, 1)
	_, err = conn.Read(b)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal([]byte{byte(RaftRPC)}, b) {
		return nil, fmt.Errorf("not a raft rpc")
	}
	if s.serverTLSConfig != nil {
		return tls.Server(conn, s.serverTLSConfig), nil
	}

	return conn, nil
}

// Close closes the listener.
func (s *StreamLayer) Close() error {
	return s.ln.Close()
}

// Addr returns the listener's network address.
func (s *StreamLayer) Addr() net.Addr {
	return s.ln.Addr()
}
