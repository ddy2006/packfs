// Package validate checks arcset DATA and EC shards in PostgreSQL placement.
package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options controls validation.
type Options struct {
	ArcsetID int64
	Stage    string // data | ec | all
}

// Run validates an arcset and updates health/status on success.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	if opts.Stage == "" {
		opts.Stage = "all"
	}
	if opts.Stage != "data" && opts.Stage != "ec" && opts.Stage != "all" {
		return fmt.Errorf("stage must be data, ec, or all")
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
	if opts.Stage == "ec" && meta.m == 0 {
		return fmt.Errorf("arcset has no EC (m=0)")
	}
	rows, err := store.ListShards(ctx, opts.ArcsetID)
	if err != nil {
		return err
	}
	relations, err := store.ListRelations(ctx, opts.ArcsetID)
	if err != nil {
		return err
	}

	if opts.Stage == "data" || opts.Stage == "all" {
		if err := validateData(a, rows); err != nil {
			return err
		}
		if opts.Stage == "data" {
			return store.SetHealth(ctx, opts.ArcsetID, "ok")
		}
	}
	if opts.Stage == "all" {
		if err := validateShardCounts(rows, relations); err != nil {
			return err
		}
	}
	if meta.m > 0 && (opts.Stage == "ec" || opts.Stage == "all") {
		if err := validateEC(a, meta, rows); err != nil {
			return err
		}
	}
	if opts.Stage == "all" {
		return store.CompleteBuild(ctx, opts.ArcsetID)
	}
	if err := store.SetParityDegraded(ctx, opts.ArcsetID, false); err != nil {
		return err
	}
	return store.SetHealth(ctx, opts.ArcsetID, "ok")
}

func validateData(a *arcset.Arcset, rows []*arcset.ShardRow) error {
	for _, row := range rows {
		if row.Type != "DATA" {
			continue
		}
		path := filepath.Join(a.CurrentPath, row.FilePath)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("DATA shard %s: %w", row.FilePath, err)
		}
		if row.FileSize > 0 && info.Size() != row.FileSize {
			return fmt.Errorf("DATA shard %s size mismatch: db=%d disk=%d",
				row.FilePath, row.FileSize, info.Size())
		}
		if row.SHA256 != nil && *row.SHA256 != "" {
			sum, err := ec.SHA256File(path)
			if err != nil {
				return err
			}
			if sum != *row.SHA256 {
				return fmt.Errorf("DATA shard %s checksum mismatch", row.FilePath)
			}
		}
	}
	return nil
}

func validateShardCounts(rows []*arcset.ShardRow, relations []arcset.RelationDatasetRow) error {
	counts := map[int64]int{}
	for _, row := range rows {
		if row.Type == "DATA" && row.DatasetRef != nil {
			counts[*row.DatasetRef]++
		}
	}
	for _, rel := range relations {
		expected := metaInt64(rel.Metadata, "shard_count")
		if expected > 0 && int64(counts[rel.DatasetRef]) < expected {
			return fmt.Errorf("dataset %s shard_count incomplete: expected=%d actual=%d",
				rel.UID, expected, counts[rel.DatasetRef])
		}
	}
	return nil
}

type stripeKey struct {
	group  int
	stripe int
}

func validateEC(a *arcset.Arcset, meta stripeMeta, rows []*arcset.ShardRow) error {
	stripes := map[stripeKey]map[int]*arcset.ShardRow{}
	for _, row := range rows {
		if row.GroupNo == nil || row.StripeSeq == nil || row.Pos == nil {
			continue
		}
		key := stripeKey{*row.GroupNo, *row.StripeSeq}
		if stripes[key] == nil {
			stripes[key] = map[int]*arcset.ShardRow{}
		}
		stripes[key][*row.Pos] = row
	}
	for key, positions := range stripes {
		padded := int64(0)
		for _, row := range positions {
			if n := metaInt64(row.Metadata, "padded_size"); n > padded {
				padded = n
			}
			if row.Type == "EC" && row.FileSize > padded {
				padded = row.FileSize
			}
		}
		if padded <= 0 {
			return fmt.Errorf("stripe group=%d seq=%d has no padded_size", key.group, key.stripe)
		}
		files := make([]ec.StreamFile, meta.k+meta.m)
		for pos := 0; pos < meta.k+meta.m; pos++ {
			row := positions[pos]
			if row == nil {
				return fmt.Errorf("stripe group=%d seq=%d missing pos=%d", key.group, key.stripe, pos)
			}
			typ := byte('D')
			if pos >= meta.k {
				typ = 'E'
			} else if row.Type == "PAD" {
				typ = 'P'
			}
			files[pos] = ec.StreamFile{
				Path: filepath.Join(a.CurrentPath, row.FilePath),
				Size: row.FileSize,
				Type: typ,
			}
		}
		ok, err := ec.VerifyStripeStreaming(files, meta.k, meta.m, padded, ec.DefaultChunkSize)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("stripe group=%d seq=%d verification failed", key.group, key.stripe)
		}
	}
	return nil
}

type stripeMeta struct {
	k, m int
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
	return stripeMeta{k: k, m: mcount}, nil
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
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
