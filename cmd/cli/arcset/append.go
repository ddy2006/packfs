package arcset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func appendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a dataset to an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			datasetDBPath, _ := cmd.Flags().GetString("dataset-db")
			if datasetDBPath == "" {
				return errors.NewUsage("--dataset-db is required")
			}
			datasetKey, _ := cmd.Flags().GetString("dataset-key")

			absPath, err := filepath.Abs(datasetDBPath)
			if err != nil {
				return errors.WrapE(err, "resolve dataset sqlite path")
			}
			dsDB, err := db.OpenDatasetSQLite(absPath, true)
			if err != nil {
				return errors.WrapE(err, "open dataset sqlite", "path", absPath)
			}
			dsStore := dataset.NewDatasetStore(dsDB)
			ds, err := findSingleDataset(context.Background(), dsStore)
			if err != nil {
				dsDB.Close()
				return err
			}
			shardStore := shard.NewDatasetStore(dsDB)
			shards, err := shardStore.FindByDataset(context.Background(), ds.ID)
			dsDB.Close()
			if err != nil {
				return errors.WrapE(err, "list dataset shards")
			}
			if ds.Status != "built" {
				return errors.E("dataset is not built", "status", ds.Status)
			}
			if len(shards) == 0 {
				return errors.E("dataset has no DATA shards")
			}
			if expected := metaInt64(ds.Metadata, "shard_count"); expected > 0 && int64(len(shards)) < expected {
				return errors.E("dataset shard_count is incomplete",
					"expected", expected, "actual", len(shards))
			}
			if datasetKey == "" {
				datasetKey = dataset.DiscoveredDatasetKey(ds.UID)
			}

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			store := arcset.NewPGStore(pool)
			a, err := store.FindByID(ctx, id)
			if err != nil {
				return errors.WrapE(err, "find arcset", "id", id)
			}
			if err := inheritArcsetConfig(ctx, store, a, ds); err != nil {
				return err
			}

			rel, err := store.AppendDataset(ctx, arcset.AppendDatasetParams{
				ArcsetID:        id,
				UID:             ds.UID,
				Name:            ds.Name,
				SQLitePath:      absPath,
				DatasetKey:      datasetKey,
				DatasetMetadata: ds.Metadata,
			})
			if err != nil {
				return errors.WrapE(err, "append dataset")
			}

			// Freeze after PG registration. If this fails, a retry will find
			// the existing relation and repeat the freeze.
			if err := os.Chmod(absPath, 0o444); err != nil {
				return errors.WrapE(err, "freeze dataset sqlite", "path", absPath)
			}

			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"arcset_id":    id,
				"dataset_uid":  ds.UID,
				"dataset_key":  rel.DatasetKey,
				"append_order": rel.AppendOrder,
			})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("dataset-db", "", "path to dataset SQLite")
	cmd.Flags().String("dataset-key", "", "dataset key within the arcset")
	return cmd
}

func findSingleDataset(ctx context.Context, store *dataset.DatasetStore) (*dataset.Dataset, error) {
	limit := 1
	all, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		return nil, errors.WrapE(err, "list dataset")
	}
	if len(all) == 0 {
		return nil, errors.E("dataset sqlite contains no dataset")
	}
	return all[0], nil
}

func inheritArcsetConfig(ctx context.Context, store *arcset.PGStore, a *arcset.Arcset, ds *dataset.Dataset) error {
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
			return errors.E("dataset config mismatch", "key", key,
				"arcset", target, "dataset", source)
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
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}
