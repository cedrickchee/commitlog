package log

import (
	"fmt"
	"os"
	"path"

	api "github.com/cedrickchee/commitlog/api/v1"
	"google.golang.org/protobuf/proto"
)

// Segment is the abstraction that ties a store and an index together.

// Segment wraps the index and store types to coordinate operations across the
// two.
//
// For example, when the log appends a record to the active segment, the segment
// needs to write the data to its store and add a new entry in the index.
// Similarly for reads, the segment needs to look up the entry from the index
// and then fetch the data from the store.
type segment struct {
	// Segment needs to call its store and index files, so we keep pointers to
	// those in the first two fields.
	store *store // the file we store records in
	index *index // the file we store index entries in

	// We need the next and base offsets to know what offset to append new
	// records under and to calculate the relative offsets for the index
	// entries.
	baseOffset, nextOffset uint64

	// We put the config on the segment so we can compare the store file and
	// index sizes to the configured limits, which lets us know when the segment
	// is maxed out.
	config Config
}

// newSegment adds a new segment, such as when the current active segment hits
// its max size.
func newSegment(dir string, baseOffset uint64, c Config) (*segment, error) {
	s := &segment{
		baseOffset: baseOffset,
		config:     c,
	}

	// Open the store and index files and pass the `os.O_CREATE` file mode flag
	// as an argument to `os.OpenFile()` to create the files if they don't exist
	// yet.

	var err error
	// When we create the store file, we pass the `os.O_APPEND` flag to make the
	// OS append to the file when writing. Then we create our index and store
	// with these files.
	storeFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".store")),
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0644,
	)
	if err != nil {
		return nil, err
	}
	if s.store, err = newStore(storeFile); err != nil {
		return nil, err
	}
	indexFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".index")),
		os.O_RDWR|os.O_CREATE,
		0644,
	)
	if err != nil {
		return nil, err
	}
	if s.index, err = newIndex(indexFile, c); err != nil {
		return nil, err
	}

	// Set the segment's next offset to prepare for the next appended record.
	if off, _, err := s.index.Read(-1); err != nil {
		// If the index is empty, then the next record appended to the segment
		// would be the first record and its offset would be the segment's base
		// offset.
		s.nextOffset = baseOffset
	} else {
		// If the index has at least one entry, then that means the offset of
		// the next record written should take the offset at the end of the
		// segment, which we get by adding 1 to the base offset and relative
		// offset.
		s.nextOffset = baseOffset + uint64(off) + 1
	}
	return s, nil
}

// Append writes the record to the segment and returns the newly appended
// record's offset. record is the data stored in our log.
func (s *segment) Append(record *api.Record) (offset uint64, err error) {
	cur := s.nextOffset
	record.Offset = cur
	p, err := proto.Marshal(record)
	if err != nil {
		return 0, err
	}

	// The segment appends a record in a two-step process: it appends the data
	// to the store and then adds an index entry.
	_, pos, err := s.store.Append(p)
	if err != nil {
		return 0, err
	}
	if err = s.index.Write(
		// index offsets are relative to base offset
		uint32(s.nextOffset-uint64(s.baseOffset)),
		pos,
	); err != nil {
		return 0, err
	}

	// Increment the next offset to prep for a future append call.
	s.nextOffset++

	return cur, nil
}

// Read returns the record for the given offset.
func (s *segment) Read(off uint64) (*api.Record, error) {
	// To read a record the segment must first translate the absolute index into
	// a relative offset and get the associated index entry.
	_, pos, err := s.index.Read(int64(off - s.baseOffset))
	if err != nil {
		return nil, err
	}
	// Once it has the index entry, the segment can go straight to the record’s
	// position in the store and read the proper amount of data.
	p, err := s.store.Read(pos)
	if err != nil {
		return nil, err
	}
	record := &api.Record{}
	err = proto.Unmarshal(p, record)
	return record, err
}

// IsMaxed returns whether the segment has reached its max size, either by
// writing too much to the store or the index.
func (s *segment) IsMaxed() bool {
	// If you wrote a small number of long logs, then you'd hit the segment
	// bytes limit; if you wrote a lot of small logs, then you'd hit the index
	// bytes limit. The log uses this method to know it needs to create a new
	// segment.
	return s.store.size >= s.config.Segment.MaxStoreBytes ||
		s.index.size >= s.config.Segment.MaxIndexBytes
}

// Close closes the segment.
func (s *segment) Close() error {
	if err := s.index.Close(); err != nil {
		return err
	}
	if err := s.store.Close(); err != nil {
		return err
	}
	return nil
}

// Remove closes the segment and removes the index and store files.
func (s *segment) Remove() error {
	if err := s.Close(); err != nil {
		return err
	}
	if err := os.Remove(s.index.Name()); err != nil {
		return err
	}
	if err := os.Remove(s.store.Name()); err != nil {
		return err
	}
	return nil
}

// nearestMultiple returns the nearest and lesser multiple of `k` in `j`, for
// example `nearestMultiple(9, 4) == 8`. We take the lesser multiple to make
// sure we stay under the user's disk capacity.
func nearestMultiple(j, k uint64) uint64 {
	if j >= 0 {
		return (j / k) * k
	}
	return ((j - k + 1) / k) * k
}
