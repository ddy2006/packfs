package dataset

import "context"

// Store defines the persistence interface for datasets.
type Store interface {
	// Dataset CRUD
	Create(ctx context.Context, ds *Dataset) error
	UpdateMetadata(ctx context.Context, id int, metadata map[string]any) error
	FindByName(ctx context.Context, name string) (*Dataset, error)
	Find(ctx context.Context, filter Filter) ([]*Dataset, error)

	// File records
	AddFileRecord(ctx context.Context, f *File) error
	ListFiles(ctx context.Context, datasetID int) ([]*File, error)
}
