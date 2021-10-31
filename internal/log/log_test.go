package log

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	api "github.com/cedrickchee/commitlog/api/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestLog defines a table of tests to test the log.
func TestLog(t *testing.T) {
	// Table to write the log tests so we don't have to repeat the code that
	// creates a new log for every test case.
	tcases := map[string]func(t *testing.T, log *Log){
		"append and read a record succeeds": testAppendRead,
		"offset out of range error":         testOutOfRangeErr,
		"init with existing segments":       testInitExisting,
		"reader":                            testReader,
		"truncate":                          testTruncate,
	}
	for scenario, fn := range tcases {
		t.Run(scenario, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "store_test")
			require.NoError(t, err)
			defer os.RemoveAll(dir)

			c := Config{}
			c.Segment.MaxStoreBytes = 32
			log, err := NewLog(dir, c)
			require.NoError(t, err)

			fn(t, log)
		})
	}
}

// testAppendRead tests that we can successfully append to and read from the
// log.
func testAppendRead(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hey, this is a test"),
	}

	// When we append a record to the log, the log returns the offset it
	// associated that record with. So, when we ask the log for the record at
	// that offset, we expect to get the same record that we appended.
	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	read, err := log.Read(off)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

// testOutOfRangeErr tests that the log returns an error when we try to read an
// offset that's outside of the range of offsets the log has stored.
func testOutOfRangeErr(t *testing.T, log *Log) {
	read, err := log.Read(1)
	require.Nil(t, read)
	apiErr := err.(api.ErrOffsetOutOfRange)
	require.Equal(t, uint64(1), apiErr.Offset)
}

// testInitExisting tests that when we create a log, the log bootstraps itself
// from the data stored by prior log instances.
func testInitExisting(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hey, this is a test"),
	}

	// We append three records to the original log before closing it.
	for i := 0; i < 3; i++ {
		_, err := log.Append(append)
		require.NoError(t, err)
	}
	require.NoError(t, log.Close())

	off, err := log.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)
	off, err = log.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	// Then we create a new log configured with the same directory as the old log.
	l, err := NewLog(log.Dir, log.Config)
	require.NoError(t, err)

	// Finally, we confirm that the new log set itself up from the data stored by the original log.
	off, err = l.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)
	off, err = l.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)
}

// testReader tests that we can read the full, raw log as it's stored on disk so that we can
// snapshot and restore the logs.
func testReader(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hey, this is a test"),
	}
	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	reader := log.Reader()
	b, err := io.ReadAll(reader)
	require.NoError(t, err)

	read := &api.Record{}
	err = proto.Unmarshal(b[lenWidth:], read)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

// testTruncate tests that we can truncate the log and remove old segments that we don’t need any
// more.
func testTruncate(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hey, this is a test"),
	}
	for i := 0; i < 3; i++ {
		_, err := log.Append(append)
		require.NoError(t, err)
	}

	err := log.Truncate(1)
	require.NoError(t, err)

	_, err = log.Read(0)
	require.Error(t, err)
}
