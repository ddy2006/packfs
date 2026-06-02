package arcset

import (
	"context"

	"github.com/kaichao/gopkg/errors"
)

// GenerateShardDefs groups files by dataset, then by shard_max_bytes into shards.
// If a single file exceeds shard_max_bytes, it is split into multiple segments.
// Otherwise files are kept whole. A shard never spans multiple datasets.
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
			// 文件不超过 maxBytes：整体处理
			if maxBytes == 0 || f.FileSize <= maxBytes {
				// 当前 shard 放不下 → 关闭，新建
				if maxBytes > 0 && currentSize+f.FileSize > maxBytes && currentSize > 0 {
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
				continue
			}

			// 文件超过 maxBytes → 拆分
			remaining := f.FileSize
			offset := int64(0)
			for remaining > 0 {
				if maxBytes > 0 && currentSize >= maxBytes {
					emit()
				}
				chunk := min(remaining, maxBytes)
				spaceLeft := maxBytes - currentSize
				if currentSize > 0 && chunk > spaceLeft {
					chunk = spaceLeft
					if chunk == 0 {
						emit()
						chunk = min(remaining, maxBytes)
					}
				}
				current.Segments = append(current.Segments, SegmentDesc{
					FilePath:    f.FilePath,
					FileSize:    f.FileSize,
					FileOffset:  offset,
					SegmentSize: chunk,
					FileID:      f.ID,
				})
				currentSize += chunk
				remaining -= chunk
				offset += chunk

				if maxBytes > 0 && currentSize >= maxBytes {
					emit()
				}
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
