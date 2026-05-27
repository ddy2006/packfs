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
	Backend   string
	Metadata  map[string]any
	LastCheck time.Time
	Arcset    int
}

// UnpackInfo is a segment position + original file path, used for unpacking.
type UnpackInfo struct {
	FilePath string // original relative path from t_file
	Offset   int64  // offset within shard
	Size     int64  // bytes to read from shard
}

// Segment represents a row in the t_segment table.
type Segment struct {
	ID           int
	ShardPath    string
	Offset       int64
	Size         int64
	Shard        int
	Arcset       int
	CompressAlgo string
	Checksum     string
	File         int
	FileOffset   int64
	FileSize     int64
}
