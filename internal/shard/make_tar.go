package shard

import (
	"archive/tar"
	"bytes"
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

func makeTar(ctx context.Context, db *sql.DB, cfg MakeConfig, segs []SegmentDef, outputPath string) (*MakeResult, error) {
	isSegmentCompress, isShardCompress, isXZ := parseCompressMode(cfg.Compress)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, errors.WrapE(err, "create output directory")
	}

	type segInfo struct {
		offset   int64
		size     int64
		csize    int64
		fileID   int
		fileSize int64
	}
	var segInfos []segInfo
	shardHash := sha256.New()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

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

		writeData := rawData
		var csize int64
		if isSegmentCompress {
			writeData, err = CompressBytes(rawData, isXZ)
			if err != nil {
				return nil, errors.WrapE(err, "compress segment", "path", seg.Path)
			}
			csize = int64(len(writeData))
		}

		offset := int64(tarBuf.Len())
		hdr := &tar.Header{
			Name: seg.Path,
			Size: int64(len(writeData)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, errors.WrapE(err, "write tar header", "path", seg.Path)
		}
		if _, err := tw.Write(writeData); err != nil {
			return nil, errors.WrapE(err, "write tar entry", "path", seg.Path)
		}

		segInfos = append(segInfos, segInfo{
			offset:   offset,
			size:     int64(len(rawData)),
			csize:    csize,
			fileID:   fileID,
			fileSize: dbSize,
		})
	}

	if err := tw.Close(); err != nil {
		return nil, errors.WrapE(err, "close tar writer")
	}

	tarData := tarBuf.Bytes()
	var shardFileSize int64

	out, err := os.Create(outputPath)
	if err != nil {
		return nil, errors.WrapE(err, "create shard file", "path", outputPath)
	}
	defer out.Close()

	if isShardCompress {
		compressed, err := CompressBytes(tarData, isXZ)
		if err != nil {
			return nil, errors.WrapE(err, "compress shard")
		}
		if _, err := out.Write(compressed); err != nil {
			return nil, errors.WrapE(err, "write compressed shard")
		}
		shardHash.Write(compressed)
		shardFileSize = int64(len(compressed))
	} else {
		if _, err := out.Write(tarData); err != nil {
			return nil, errors.WrapE(err, "write shard")
		}
		shardHash.Write(tarData)
		shardFileSize = int64(len(tarData))
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))
	relPath := filepath.Base(outputPath)

	store := NewSQLiteStore(db)
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
