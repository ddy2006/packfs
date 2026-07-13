package shard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

func makeBin(ctx context.Context, db *sql.DB, cfg MakeConfig, segs []SegmentDef, outputPath string) (*MakeResult, error) {
	isSegmentCompress, isShardCompress, isXZ := parseCompressMode(cfg.Compress)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, errors.WrapE(err, "create output directory")
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return nil, errors.WrapE(err, "create shard file", "path", outputPath)
	}
	defer out.Close()

	store := NewSQLiteStore(db)

	type segInfo struct {
		offset   int64
		size     int64
		csize    int64
		fileID   int
		fileSize int64
	}
	var segInfos []segInfo
	var totalSize int64
	shardHash := sha256.New()

	var shardCompressor io.WriteCloser
	if isShardCompress {
		shardCompressor, err = NewCompressor(out, isXZ)
		if err != nil {
			return nil, err
		}
	}

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(cfg.SourceRoot, seg.Path)
		}

		fileID, dbSize := resolveSegFileID(ctx, db, seg.Path, cfg.DatasetID)
		if info, err := os.Stat(srcPath); err == nil {
			if dbSize > 0 && info.Size() != dbSize {
				logrus.Warnf("%s: size changed since dataset creation (db=%d, disk=%d)",
					seg.Path, dbSize, info.Size())
			}
		}

		f, err := os.Open(srcPath)
		if err != nil {
			return nil, errors.WrapE(err, "open source file", "path", seg.Path)
		}

		var rawData []byte
		if seg.Size <= 0 {
			rawData, err = io.ReadAll(f)
		} else {
			rawData = make([]byte, seg.Size)
			_, err = io.ReadFull(f, rawData)
		}
		f.Close()
		if err != nil {
			return nil, errors.WrapE(err, "read source file", "path", seg.Path)
		}

		offset := totalSize
		writeData := rawData
		var csize int64

		if isSegmentCompress {
			writeData, err = CompressBytes(rawData, isXZ)
			if err != nil {
				return nil, errors.WrapE(err, "compress segment", "path", seg.Path)
			}
			csize = int64(len(writeData))
		}

		if isShardCompress {
			shardCompressor.Write(rawData)
			totalSize += int64(len(rawData))
		} else {
			out.Write(writeData)
			shardHash.Write(writeData)
			totalSize += int64(len(writeData))
		}

		segInfos = append(segInfos, segInfo{
			offset:   offset,
			size:     int64(len(rawData)),
			csize:    csize,
			fileID:   fileID,
			fileSize: dbSize,
		})
	}

	shardFileSize := totalSize
	if isShardCompress {
		if err := shardCompressor.Close(); err != nil {
			return nil, errors.WrapE(err, "close compressor")
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return nil, errors.WrapE(err, "seek shard file for hash")
		}
		if _, err := io.Copy(shardHash, out); err != nil {
			return nil, errors.WrapE(err, "hash compressed shard")
		}
		stat, _ := out.Stat()
		shardFileSize = stat.Size()
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))
	relPath := filepath.Base(outputPath)

	var shardID int
	if err := writeShardRecord(ctx, store, &shardID, cfg, relPath, shardFileSize, shardChecksum); err != nil {
		return nil, err
	}

	var segments []*Segment
	for _, si := range segInfos {
		segments = append(segments, &Segment{
			Offset:     si.offset,
			Size:       si.size,
			Csize:      si.csize,
			Shard:      shardID,
			File:       si.fileID,
			FileOffset: 0,
			FileSize:   si.fileSize,
		})
	}
	if err := store.ReplaceSegments(ctx, shardID, segments); err != nil {
		return nil, err
	}

	fmt.Printf("created shard %s (%d bytes, sha256=%s, %d segments)\n", outputPath, shardFileSize, shardChecksum, len(segInfos))
	return &MakeResult{Path: outputPath, Size: shardFileSize, Checksum: shardChecksum, Segments: len(segInfos)}, nil
}
