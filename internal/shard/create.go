package shard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateShard creates a single shard file from the given segment descriptions.
func CreateShard(
	ctx context.Context,
	store Store,
	descs []arcset.SegmentDesc,
	arcsetID int,
	seq int,
	outputDir string,
	shardType string,
	backend string,
	compressAlgo string,
) error {
	if len(descs) == 0 {
		return errors.E("no segments to pack")
	}

	shardFileName := fmt.Sprintf("shard_%d_%04d.pak", arcsetID, seq)
	shardPath := filepath.Join(outputDir, shardFileName)

	out, err := os.Create(shardPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", shardPath)
	}
	defer out.Close()

	// Write segments to the shard file, collecting segment metadata.
	type pendingSeg struct {
		seg       *Segment
		desc      arcset.SegmentDesc
	}
	var pendings []pendingSeg
	var shardSize int64
	shardHash := sha256.New()

	for _, desc := range descs {
		segChecksum, n, err := copySegmentTo(out, shardHash, desc)
		if err != nil {
			return errors.WrapE(err, "write segment to shard", "file", desc.FilePath)
		}

		pendings = append(pendings, pendingSeg{
			seg: &Segment{
				ShardPath:    shardPath,
				Offset:       shardSize,
				Size:         n,
				Arcset:       arcsetID,
				CompressAlgo: compressAlgo,
				Checksum:     segChecksum,
				File:         desc.FileID,
				FileOffset:   desc.FileOffset,
				FileSize:     desc.FileSize,
			},
			desc: desc,
		})
		shardSize += n
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	sh := &Shard{
		Seq:       seq,
		FilePath:  shardPath,
		FileSize:  shardSize,
		Type:      shardType,
		Checksum:  shardChecksum,
		Backend:   backend,
		LastCheck: time.Now(),
		Arcset:    arcsetID,
	}
	if err := store.CreateShard(ctx, sh); err != nil {
		return err
	}

	for _, p := range pendings {
		p.seg.Shard = sh.ID
		if err := store.AddSegment(ctx, p.seg); err != nil {
			return err
		}
	}

	logrus.Infof("created shard %s with %d segments (%d bytes)", shardPath, len(descs), shardSize)
	return nil
}

func copySegmentTo(w, shardHash io.Writer, desc arcset.SegmentDesc) (checksum string, written int64, err error) {
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
