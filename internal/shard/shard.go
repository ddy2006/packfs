package shard

import "time"

// Shard represents a row in the t_shard table.
type Shard struct {
	ID        int
	Seq       int
	FilePath  string
	FileSize  int64
	Type      string
	Checksum  string
	Metadata  map[string]any
	LastCheck time.Time
	Arcset    int
}

// UnpackInfo is a segment position + original file path, used for unpacking.
type UnpackInfo struct {
	FilePath string
	Offset   int64
	Size     int64
}

// Segment represents a row in the t_segment table.
type Segment struct {
	ID         int
	Offset     int64
	Size       int64
	Shard      int
	Arcset     int
	File       int
	FileOffset int64
	FileSize   int64
}
