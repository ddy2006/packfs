package arcset

import "time"

// Arcset represents a row in the t_arcset table.
type Arcset struct {
	ID           int
	Name         string
	PathRegex    string
	Label        string
	CreateTime   time.Time
	RaitType     string
	Metadata     map[string]any
	Status       string
	UnitBytes    int64
	SegmentBytes int64
	Backend      string
	SumBytes     int64
	NetBytes     int64
	CompressAlgo string
	LastCheck    time.Time
	Comment      string
}

// Filter for querying arcsets.
type Filter struct {
	Status *string
}

// Update fields for partial updates.
type Update struct {
	Status      *string
	Label       *string
	Comment     *string
	SumBytes    *int64
	NetBytes    *int64
	LastCheck   *time.Time
	Metadata    map[string]any
}

// DatasetRef is a lightweight reference to a linked dataset.
type DatasetRef struct {
	ID   int
	Name string
}

// SegmentDesc describes a file portion to be packed into a shard.
// Checksum is not included here — it is computed during distributed shard creation.
type SegmentDesc struct {
	FilePath    string
	FileSize    int64
	FileOffset  int64
	SegmentSize int64
	FileID      int
}
