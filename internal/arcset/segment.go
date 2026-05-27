package arcset

import (
	"context"

	"github.com/kaichao/gopkg/errors"
)

// GenerateShardDefs groups files by dataset, then by shard_max_bytes into shards.
// Files are never split. A shard never spans multiple datasets.
func GenerateShardDefs(ctx context.Context, store Store, arcsetID int) ([]ShardDef, error) {
	a, err := store.FindByID(ctx, arcsetID)
	if err != nil {
		return nil, errors.WrapE(err, "find arcset for shard generation", "arcset_id", arcsetID)
	}

	files, err := store.ListArcsetFiles(ctx, arcsetID)
	if err != nil {
		return nil, err
	}

	maxBytes := getInt64Meta(a.Metadata, "shard_max_bytes")

	// 按 dataset 分组
	groups := make(map[int][]FileRow)
	var order []int
	for _, f := range files {
		if _, ok := groups[f.DatasetID]; !ok {
			order = append(order, f.DatasetID)
		}
		groups[f.DatasetID] = append(groups[f.DatasetID], f)
	}

	var shards []ShardDef

	for _, dsID := range order {
		fs := groups[dsID]
		var current ShardDef
		var currentSize int64

		emit := func() {
			if len(current.Segments) > 0 {
				current.DatasetID = dsID
				shards = append(shards, current)
				current = ShardDef{}
				currentSize = 0
			}
		}

		for _, f := range fs {
			if maxBytes > 0 && currentSize > 0 && currentSize+f.FileSize > maxBytes {
				emit()
			}

			current.Segments = append(current.Segments, SegmentDesc{
				FilePath:    f.FilePath,
				FileSize:    f.FileSize,
				FileOffset:  0,
				SegmentSize: f.FileSize,
				FileID:      f.ID,
			})
			currentSize += f.FileSize

			if maxBytes > 0 && f.FileSize > maxBytes {
				emit()
			}
		}
		emit()
	}

	for i := range shards {
		shards[i].Seq = i
	}

	return shards, nil
}

func getInt64Meta(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
