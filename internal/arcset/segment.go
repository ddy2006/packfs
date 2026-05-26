package arcset

import (
	"context"

	"github.com/kaichao/gopkg/errors"
)

// GenerateSegments queries all files linked to an arcset and produces a list
// of SegmentDesc for distributed shard creation. Files larger than segment_bytes
// are split into multiple segments.
func GenerateSegments(ctx context.Context, store Store, arcsetID int) ([]SegmentDesc, error) {
	a, err := store.FindByID(ctx, arcsetID)
	if err != nil {
		return nil, errors.WrapE(err, "find arcset for segment generation", "arcset_id", arcsetID)
	}

	files, err := store.ListArcsetFiles(ctx, arcsetID)
	if err != nil {
		return nil, err
	}

	segBytes := a.SegmentBytes
	if segBytes <= 0 {
		segBytes = a.UnitBytes
	}
	if segBytes <= 0 {
		return nil, errors.E("segment_bytes not configured for arcset", "arcset", a.Name)
	}

	var descs []SegmentDesc
	for _, f := range files {
		remaining := f.FileSize
		for offset := int64(0); offset < f.FileSize; offset += segBytes {
			sz := segBytes
			if remaining < segBytes {
				sz = remaining
			}
			descs = append(descs, SegmentDesc{
				FilePath:    f.FilePath,
				FileSize:    f.FileSize,
				FileOffset:  offset,
				SegmentSize: sz,
				FileID:      f.ID,
			})
			remaining -= sz
		}
	}
	return descs, nil
}
