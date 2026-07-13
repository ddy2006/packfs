package shard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateShard creates a single shard file from the given segment descriptions.
func CreateShard(
	ctx context.Context,
	store Store,
	descs []dataset.SegmentDesc,
	arcsetID, datasetID int,
	seq int,
	outputDir string,
	shardType string,
) error {
	if len(descs) == 0 {
		return errors.E("no segments to pack")
	}

	shardRelPath := fmt.Sprintf("shard_%d_%04d.pak", arcsetID, seq)
	shardAbsPath := filepath.Join(outputDir, shardRelPath)

	out, err := os.Create(shardAbsPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", shardAbsPath)
	}
	defer out.Close()

	type pendingSeg struct {
		seg  *Segment
		desc dataset.SegmentDesc
	}
	var pendings []pendingSeg
	var shardSize int64
	shardHash := sha256.New()

	for _, desc := range descs {
		_, n, err := copySegmentTo(out, shardHash, desc)
		if err != nil {
			return errors.WrapE(err, "write segment to shard", "file", desc.FilePath)
		}

		pendings = append(pendings, pendingSeg{
			seg: &Segment{
				Offset:     shardSize,
				Size:       n,
				File:       desc.FileID,
				FileOffset: desc.FileOffset,
				FileSize:   desc.FileSize,
			},
			desc: desc,
		})
		shardSize += n
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	sh := &Shard{
		Seq:       seq,
		FilePath:  shardRelPath,
		FileSize:  shardSize,
		Type:      shardType,
		Checksum:  shardChecksum,
		LastCheck: time.Now(),
		Arcset:    sql.NullInt64{Int64: int64(arcsetID), Valid: arcsetID > 0},
		Dataset:   sql.NullInt64{Int64: int64(datasetID), Valid: datasetID > 0},
	}
	if err := store.CreateShard(ctx, sh); err != nil {
		return err
	}

	var segs []*Segment
	for _, p := range pendings {
		segs = append(segs, p.seg)
	}
	if err := store.ReplaceSegments(ctx, sh.ID, segs); err != nil {
		return err
	}

	logrus.Infof("created shard %s with %d segments (%d bytes)", shardAbsPath, len(descs), shardSize)
	return nil
}

func copySegmentTo(w, shardHash io.Writer, desc dataset.SegmentDesc) (checksum string, written int64, err error) {
	f, err := os.Open(desc.FilePath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	if desc.FileOffset > 0 {
		if _, err := f.Seek(desc.FileOffset, io.SeekStart); err != nil {
			return "", 0, err
		}
	}

	segHash := sha256.New()
	mw := io.MultiWriter(w, shardHash, segHash)
	n, err := io.CopyN(mw, f, desc.SegmentSize)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return fmt.Sprintf("%x", segHash.Sum(nil)), n, err
}
