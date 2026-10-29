// Package recoverec reconstructs missing DATA/EC shards from a stripe.
package recoverec

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options controls recovery.
type Options struct {
	ArcsetID  int64
	ShardFile string
	Target    string // data | all
	ChunkSize int
}

// Run recovers one missing shard.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	if opts.Target == "" {
		opts.Target = "all"
	}
	if opts.Target != "data" && opts.Target != "all" {
		return fmt.Errorf("target must be data or all")
	}
	if opts.ChunkSize == 0 {
		opts.ChunkSize = ec.DefaultChunkSize
	}
	if err := ec.ValidateChunkSize(opts.ChunkSize); err != nil {
		return err
	}
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, opts.ArcsetID)
	if err != nil {
		return fmt.Errorf("find arcset: %w", err)
	}
	meta, err := parseMeta(a.Metadata)
	if err != nil {
		return err
	}
	if meta.m == 0 {
		return fmt.Errorf("arcset has no EC (m=0)")
	}

	lostPath := normalizePath(a.CurrentPath, opts.ShardFile)
	lost, err := store.FindShardByPath(ctx, opts.ArcsetID, lostPath)
	if err != nil {
		return err
	}
	if lost == nil {
		return fmt.Errorf("shard %s not found in arcset", lostPath)
	}
	if lost.GroupNo == nil || lost.StripeSeq == nil || lost.Pos == nil {
		return fmt.Errorf("shard %s has no stripe placement", lostPath)
	}
	if opts.Target == "data" && lost.Type != "DATA" {
		return fmt.Errorf("--target=data only recovers DATA shards")
	}

	rows, err := store.ListShards(ctx, opts.ArcsetID)
	if err != nil {
		return err
	}
	posRows := map[int]*arcset.ShardRow{}
	var padded int64
	for _, row := range rows {
		if row.GroupNo == nil || row.StripeSeq == nil || row.Pos == nil {
			continue
		}
		if *row.GroupNo != *lost.GroupNo || *row.StripeSeq != *lost.StripeSeq {
			continue
		}
		posRows[*row.Pos] = row
		if n := metaInt64(row.Metadata, "padded_size"); n > padded {
			padded = n
		}
		if row.Type == "EC" && row.FileSize > padded {
			padded = row.FileSize
		}
	}
	if padded <= 0 {
		return fmt.Errorf("stripe has no padded_size metadata")
	}

	files := make([]ec.StreamFile, meta.k+meta.m)
	originalSizes := make([]int64, meta.k)
	for pos := 0; pos < meta.k+meta.m; pos++ {
		row := posRows[pos]
		path := ""
		typ := byte('D')
		size := int64(0)
		if pos >= meta.k {
			typ = 'E'
		}
		if row != nil {
			path = filepath.Join(a.CurrentPath, row.FilePath)
			switch row.Type {
			case "DATA":
				typ = 'D'
				size = metaInt64(row.Metadata, "original_size")
				if size == 0 {
					size = row.FileSize
				}
			case "EC":
				typ = 'E'
				size = row.FileSize
			case "PAD":
				typ = 'P'
			}
		} else if pos < meta.k {
			// A missing DATA position without a PG row means the stripe has
			// an unfilled data position. Treat it as PAD when it has a file.
			typ = 'P'
			part := partition(meta.layout, *lost.GroupNo, pos, *lost.StripeSeq, meta.k, meta.m)
			path = filepath.Join(a.CurrentPath,
				fmt.Sprintf("%0*d", meta.partitionWidth, part),
				fmt.Sprintf("%0*dP%s", meta.seqWidth, *lost.StripeSeq, shardExt(meta.format, meta.compress)))
		} else {
			part := partition(meta.layout, *lost.GroupNo, pos, *lost.StripeSeq, meta.k, meta.m)
			path = filepath.Join(a.CurrentPath,
				fmt.Sprintf("%0*d", meta.partitionWidth, part),
				fmt.Sprintf("%0*dE%s", meta.seqWidth, *lost.StripeSeq, shardExt(meta.format, meta.compress)))
		}
		if pos < meta.k {
			originalSizes[pos] = size
		}
		files[pos] = ec.StreamFile{Path: path, Size: size, Type: typ}
	}

	// Ensure a lost DATA shard has a writable target path.
	if files[*lost.Pos].Path == "" {
		files[*lost.Pos].Path = filepath.Join(a.CurrentPath, lost.FilePath)
	}

	if err := ec.ReconstructStripeStreaming(files, originalSizes, meta.k, meta.m,
		padded, opts.ChunkSize, opts.Target); err != nil {
		return err
	}

	if lost.Type == "DATA" && lost.SHA256 != nil {
		sum, err := ec.SHA256File(filepath.Join(a.CurrentPath, lost.FilePath))
		if err != nil {
			return err
		}
		if sum != *lost.SHA256 {
			return fmt.Errorf("recovered DATA shard checksum mismatch")
		}
	}
	if opts.Target == "all" {
		ok, err := ec.VerifyStripeStreaming(files, meta.k, meta.m, padded, opts.ChunkSize)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("stripe verification failed after recovery")
		}
	}
	if err := store.UpdateShardLastCheck(ctx, lost.ID, time.Now()); err != nil {
		return err
	}
	if err := store.SetHealth(ctx, opts.ArcsetID, "ok"); err != nil {
		return err
	}
	return store.SetParityDegraded(ctx, opts.ArcsetID, opts.Target == "data")
}

type stripeMeta struct {
	k, m           int
	partitionWidth int
	seqWidth       int
	layout         string
	format         string
	compress       string
}

func parseMeta(m map[string]any) (stripeMeta, error) {
	parts := strings.SplitN(metaString(m, "ec"), "+", 2)
	if len(parts) != 2 {
		return stripeMeta{}, fmt.Errorf("invalid ec metadata")
	}
	k, err := strconv.Atoi(parts[0])
	if err != nil {
		return stripeMeta{}, fmt.Errorf("invalid ec k")
	}
	mcount, err := strconv.Atoi(parts[1])
	if err != nil {
		return stripeMeta{}, fmt.Errorf("invalid ec m")
	}
	out := stripeMeta{
		k:              k,
		m:              mcount,
		partitionWidth: metaInt(m, "partition_width"),
		seqWidth:       metaInt(m, "seq_width"),
		layout:         metaString(m, "layout"),
		format:         metaString(m, "format"),
		compress:       metaString(m, "compress"),
	}
	if out.partitionWidth == 0 {
		out.partitionWidth = 2
	}
	if out.seqWidth == 0 {
		out.seqWidth = 3
	}
	if out.layout == "" {
		out.layout = "raid3"
	}
	if out.format == "" {
		out.format = "bin"
	}
	return out, nil
}

func partition(layout string, group, pos, stripe, k, m int) int {
	if layout == "raid5" {
		return group*(k+m) + ((pos + stripe) % (k + m))
	}
	return group*(k+m) + pos
}

func shardExt(format, compress string) string {
	if format == "" {
		format = "bin"
	}
	algo := ""
	switch compress {
	case "zstd", "zstd_seekable", "segment:zstd":
		algo = "zst"
	case "xz", "segment:xz":
		algo = "xz"
	}
	switch compress {
	case "zstd", "xz", "zstd_seekable":
		return "." + format + "." + algo
	case "segment:zstd", "segment:xz":
		return "." + algo + "." + format
	default:
		return "." + format
	}
}

func normalizePath(root, p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func metaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func metaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
