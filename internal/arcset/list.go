package arcset

import (
	"context"

	"github.com/kaichao/gopkg/errors"
)

// ListArcsets returns all arcsets with optional status filter.
func ListArcsets(ctx context.Context, store Store, filter Filter) ([]*Arcset, error) {
	arcsets, err := store.Find(ctx, filter)
	return arcsets, errors.WrapE(err, "list arcsets")
}

// ListArcsetDatasets returns the datasets linked to an arcset.
func ListArcsetDatasets(ctx context.Context, store Store, arcsetID int) ([]DatasetRef, error) {
	refs, err := store.ListDatasetRefs(ctx, arcsetID)
	return refs, errors.WrapE(err, "list arcset datasets", "arcset_id", arcsetID)
}
