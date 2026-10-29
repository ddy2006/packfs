package dataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func markBuiltCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-built",
		Short: "Validate DATA shards and mark a dataset built",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := cmd.Flags().GetString("dataset-db")
			if dbPath == "" {
				return errors.NewUsage("--dataset-db is required")
			}
			absPath, err := filepath.Abs(dbPath)
			if err != nil {
				return errors.WrapE(err, "resolve dataset sqlite path")
			}
			datasetDB, err := db.OpenDatasetSQLite(absPath, false)
			if err != nil {
				return errors.WrapE(err, "open dataset sqlite")
			}
			defer datasetDB.Close()

			ctx := context.Background()
			dsStore := dataset.NewDatasetStore(datasetDB)
			limit := 1
			all, err := dsStore.Find(ctx, dataset.Filter{Limit: &limit})
			if err != nil {
				return errors.WrapE(err, "list datasets")
			}
			if len(all) == 0 {
				return errors.E("dataset sqlite contains no dataset")
			}
			ds := all[0]
			shardStore := shard.NewDatasetStore(datasetDB)
			shards, err := shardStore.FindByDataset(ctx, ds.ID)
			if err != nil {
				return errors.WrapE(err, "list dataset shards")
			}
			if len(shards) == 0 {
				return errors.E("dataset has no DATA shards")
			}
			root, _ := ds.Metadata["shard_root"].(string)
			if root == "" {
				root = ds.CurrentPath
			}
			for _, sh := range shards {
				sum, err := ec.SHA256File(filepath.Join(root, sh.FilePath))
				if err != nil {
					return errors.WrapE(err, "validate shard", "file", sh.FilePath)
				}
				if sh.Checksum != "" && sum != sh.Checksum {
					return errors.E("shard checksum mismatch", "file", sh.FilePath)
				}
			}
			if ds.Metadata == nil {
				ds.Metadata = map[string]any{}
			}
			ds.Metadata["shard_count"] = len(shards)
			if err := dsStore.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
				return errors.WrapE(err, "update shard_count")
			}
			if err := dsStore.UpdateStatus(ctx, ds.ID, "built"); err != nil {
				return errors.WrapE(err, "mark dataset built")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"dataset_uid": ds.UID,
				"status":      "built",
				"shard_count": len(shards),
			})
		},
	}
	cmd.Flags().String("dataset-db", "", "path to dataset SQLite")
	return cmd
}
