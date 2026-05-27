package dataset

import "time"

// Dataset represents a row in the t_dataset table.
type Dataset struct {
	ID           int
	Name         string
	RelativePath string
	Label        string
	Metadata     map[string]interface{}
}

// File represents a row in the t_file table.
type File struct {
	FilePath string
	FileSize int64
	Metadata map[string]interface{}
	Ctime    time.Time
	Mtime    time.Time
	Checksum string
	Dataset  int
}

// Filter for querying datasets.
type Filter struct {
	ID   *int
	Name *string
	Limit *int
}
