package dataset

import (
	"context"

	"github.com/kaichao/gopkg/errors"
)

// ListDatasets returns all datasets with optional limit filter.
func ListDatasets(ctx context.Context, store Store, filter Filter) ([]*Dataset, error) {
	datasets, err := store.Find(ctx, filter)
	return datasets, errors.WrapE(err, "list datasets")
}
