package dataset

// Dataset represents a row in the t_dataset table.
type Dataset struct {
	ID          int
	Name        string
	Label       string
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

// Filter for querying datasets.
type Filter struct {
	ID    *int
	Name  *string
	Limit *int
}
