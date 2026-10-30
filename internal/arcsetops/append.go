// Package arcsetops implements arcset lifecycle operations shared by the CLI
// and HTTP API.
package arcsetops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppendOptions controls dataset registration.
type AppendOptions struct {
	ArcsetID   int64
	DatasetDB  string
	DatasetKey string
}

// AppendResult is the outcome of dataset registration.
type AppendResult struct {
	Relation   *arcset.Relation
	Dataset    *dataset.Dataset
	DatasetKey string
}

// Append registers a dataset SQLite in PG and freezes the SQLite file.
func Append(ctx context.Context, pool *pgxpool.Pool, opts AppendOptions) (*AppendResult, error) {
	if opts.DatasetDB == "" {
		return nil, fmt.Errorf("dataset db is required")
	}
	absPath, err := filepath.Abs(opts.DatasetDB)
	if err != nil {
		return nil, fmt.Errorf("resolve dataset sqlite path: %w", err)
	}
	infos, err := datasetops.LoadInfo(ctx, absPath)
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("dataset sqlite contains no dataset")
	}
	ds := infos[0].Dataset
	if ds.Status != "built" {
		return nil, fmt.Errorf("dataset is not built (status=%s)", ds.Status)
	}
	if infos[0].ShardCount == 0 {
		return nil, fmt.Errorf("dataset has no DATA shards")
	}
	if expected := metaInt64(ds.Metadata, "shard_count"); expected > 0 && int64(infos[0].ShardCount) < expected {
		return nil, fmt.Errorf("dataset shard_count is incomplete (expected=%d actual=%d)",
			expected, infos[0].ShardCount)
	}

	key := opts.DatasetKey
	if key == "" {
		key = dataset.DiscoveredDatasetKey(ds.UID)
	}

	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, opts.ArcsetID)
	if err != nil {
		return nil, fmt.Errorf("find arcset: %w", err)
	}
	if err := inheritConfig(ctx, store, a, ds); err != nil {
		return nil, err
	}
	rel, err := store.AppendDataset(ctx, arcset.AppendDatasetParams{
		ArcsetID:        opts.ArcsetID,
		UID:             ds.UID,
		Name:            ds.Name,
		SQLitePath:      absPath,
		DatasetKey:      key,
		DatasetMetadata: ds.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("append dataset: %w", err)
	}

	// Freeze after PG registration; a retry will find the relation and repeat.
	if err := os.Chmod(absPath, 0o444); err != nil {
		return nil, fmt.Errorf("freeze dataset sqlite %s: %w", absPath, err)
	}
	return &AppendResult{Relation: rel, Dataset: ds, DatasetKey: rel.DatasetKey}, nil
}

func inheritConfig(ctx context.Context, store *arcset.PGStore, a *arcset.Arcset,
	ds *dataset.Dataset) error {
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	changed := false
	for _, key := range []string{"format", "compress", "shard_max_bytes"} {
		target, ok := a.Metadata[key]
		if !ok {
			if v, exists := ds.Metadata[key]; exists {
				a.Metadata[key] = v
				changed = true
			}
			continue
		}
		source, exists := ds.Metadata[key]
		if !exists {
			continue
		}
		if fmt.Sprint(target) != fmt.Sprint(source) {
			return fmt.Errorf("dataset config mismatch: %s arcset=%v dataset=%v",
				key, target, source)
		}
	}
	if changed {
		return store.UpdateMetadata(ctx, int64(a.ID), a.Metadata)
	}
	return nil
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
