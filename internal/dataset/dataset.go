package dataset

// Dataset represents a row in the t_dataset table.
type Dataset struct {
	ID          int
	Name        string
	Label       string
	Status      string
	Metadata    map[string]any
	CurrentPath string
	Comment     string
}

// File represents a row in the t_file table.
type File struct {
	FilePath string
	FileSize int64
	Metadata map[string]any
	Checksum string
	Dataset  int
}

// SegmentDesc describes a file portion to be packed into a shard.
type SegmentDesc struct {
	FilePath    string
	FileSize    int64
	FileOffset  int64
	SegmentSize int64
	FileID      int
}

// ShardDef groups segments from a single dataset into one shard.
type ShardDef struct {
	Seq       int
	DatasetID int
	Segments  []SegmentDesc
}

// Filter for querying datasets.
type Filter struct {
	ID    *int
	Name  *string
	Limit *int
}
