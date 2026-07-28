package arcset

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// CreateArcsetParams holds the inputs for creating an arcset.
type CreateArcsetParams struct {
	Name        string
	Label       string
	CurrentPath string
	Metadata    map[string]any
}

// CreateArcset creates an arcset record (no dataset linking at create time;
// use AppendDataset to add datasets later).
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

	logrus.Infof("created arcset %s", a.Name)
	return a, nil
}
