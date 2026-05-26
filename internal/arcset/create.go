package arcset

import (
	"context"
	"time"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateArcsetParams holds the inputs for creating an arcset.
type CreateArcsetParams struct {
	Name         string
	PathRegex    string
	Label        string
	RaitType     string
	UnitBytes    int64
	SegmentBytes int64
	Backend      string
	CompressAlgo string
	Comment      string
	DatasetIDs   []int
}

// CreateArcset creates an arcset record and links it to the given datasets.
func CreateArcset(ctx context.Context, store Store, params CreateArcsetParams) error {
	a := &Arcset{
		Name:         params.Name,
		PathRegex:    params.PathRegex,
		Label:        params.Label,
		CreateTime:   time.Now(),
		RaitType:     params.RaitType,
		UnitBytes:    params.UnitBytes,
		SegmentBytes: params.SegmentBytes,
		Backend:      params.Backend,
		CompressAlgo: params.CompressAlgo,
		Comment:      params.Comment,
		Status:       "ON",
	}
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}

	if err := store.Create(ctx, a); err != nil {
		return err
	}

	for _, dsID := range params.DatasetIDs {
		if err := store.AddDataset(ctx, a.ID, dsID); err != nil {
			return errors.WrapE(err, "link dataset", "arcset", a.Name, "dataset_id", dsID)
		}
	}

	logrus.Infof("created arcset %s with %d datasets", a.Name, len(params.DatasetIDs))
	return nil
}
