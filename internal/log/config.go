package log

import "github.com/hashicorp/raft"

// Config is the log's configuration
type Config struct {
	Raft struct {
		raft.Config
		// Advertise Raft on FQDN -- set the address Raft bind to.
		BindAddr    string
		StreamLayer *StreamLayer
		Bootstrap   bool
	}
	Segment struct {
		MaxStoreBytes uint64
		MaxIndexBytes uint64
		InitialOffset uint64
	}
}
