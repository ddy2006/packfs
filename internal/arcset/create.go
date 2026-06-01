package arcset

import (
	"context"
	"time"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateArcsetParams holds the inputs for creating an arcset.
type CreateArcsetParams struct {
	Name        string
	Label       string
	CurrentPath string
	Metadata    map[string]any
	DatasetIDs  []int
}

// CreateArcset creates an arcset record and links it to the given datasets.
func CreateArcset(ctx context.Context, store Store, params CreateArcsetParams) (*Arcset, error) {
	metadata := params.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if _, ok := metadata["create_time"]; !ok {
		metadata["create_time"] = time.Now().Format(time.RFC3339)
	}

	a := &Arcset{
		Name:        params.Name,
		Label:       params.Label,
		CurrentPath: params.CurrentPath,
		Metadata:    metadata,
		Status:      "building",
	}

	if err := store.Create(ctx, a); err != nil {
		return nil, err
	}

	for _, dsID := range params.DatasetIDs {
		if err := store.AddDataset(ctx, a.ID, dsID); err != nil {
			return nil, errors.WrapE(err, "link dataset", "arcset", a.Name, "dataset_id", dsID)
		}
	}

	logrus.Infof("created arcset %s with %d datasets", a.Name, len(params.DatasetIDs))
	return a, nil
}
