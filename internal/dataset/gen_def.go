package dataset

// GenerateShardDefs groups files into shards based on shard_max_bytes.
// Files are kept in order; if a single file exceeds shardMaxBytes, it is split
// into multiple segments. Otherwise files are kept whole.
func GenerateShardDefs(files []*File, shardMaxBytes int64, datasetID int) []ShardDef {
	var shards []ShardDef
	var current ShardDef
	var currentSize int64

	emit := func() {
		if len(current.Segments) > 0 {
			current.DatasetID = datasetID
			shards = append(shards, current)
			current = ShardDef{}
			currentSize = 0
		}
	}

	for _, f := range files {
		// 文件不超过 maxBytes：整体处理
		if shardMaxBytes == 0 || f.FileSize <= shardMaxBytes {
			// 当前 shard 放不下 → 关闭，新建
			if shardMaxBytes > 0 && currentSize+f.FileSize > shardMaxBytes && currentSize > 0 {
				emit()
			}
			current.Segments = append(current.Segments, SegmentDesc{
				FilePath:    f.FilePath,
				FileSize:    f.FileSize,
				FileOffset:  0,
				SegmentSize: f.FileSize,
				FileID:      0, // caller should resolve FileID
			})
			currentSize += f.FileSize
			continue
		}

		// 文件超过 shardMaxBytes → 拆分
		remaining := f.FileSize
		offset := int64(0)
		for remaining > 0 {
			if shardMaxBytes > 0 && currentSize >= shardMaxBytes {
				emit()
			}
			chunk := min(remaining, shardMaxBytes)
			spaceLeft := shardMaxBytes - currentSize
			if currentSize > 0 && chunk > spaceLeft {
				chunk = spaceLeft
				if chunk == 0 {
					emit()
					chunk = min(remaining, shardMaxBytes)
				}
			}
			current.Segments = append(current.Segments, SegmentDesc{
				FilePath:    f.FilePath,
				FileSize:    f.FileSize,
				FileOffset:  offset,
				SegmentSize: chunk,
				FileID:      0, // caller should resolve FileID
			})
			currentSize += chunk
			remaining -= chunk
			offset += chunk

			if shardMaxBytes > 0 && currentSize >= shardMaxBytes {
				emit()
			}
		}
	}
	emit()

	for i := range shards {
		shards[i].Seq = i
	}

	return shards
}
