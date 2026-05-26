package arcset

import "context"

// Store defines the persistence interface for arcsets.
type Store interface {
	Create(ctx context.Context, a *Arcset) error
	FindByName(ctx context.Context, name string) (*Arcset, error)
	FindByID(ctx context.Context, id int) (*Arcset, error)
	Find(ctx context.Context, filter Filter) ([]*Arcset, error)
	Update(ctx context.Context, name string, u Update) error

	AddDataset(ctx context.Context, arcsetID, datasetID int) error
	ListDatasetRefs(ctx context.Context, arcsetID int) ([]DatasetRef, error)

	// ListArcsetFiles returns all files (t_file rows) linked to an arcset
	// via r_arcset_dataset, for use in segment generation.
	ListArcsetFiles(ctx context.Context, arcsetID int) ([]FileRow, error)
}

// FileRow is a row from t_file joined through arcset→dataset.
type FileRow struct {
	ID       int
	FilePath string
	FileSize int64
	Checksum string
}
