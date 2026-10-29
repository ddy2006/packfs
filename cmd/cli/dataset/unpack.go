package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func unpackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack a dataset from its DATA shards",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := cmd.Flags().GetString("dataset-db")
			if dbPath == "" {
				return errors.NewUsage("--dataset-db is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
			}
			absPath, err := filepath.Abs(dbPath)
			if err != nil {
				return errors.WrapE(err, "resolve dataset sqlite path")
			}
			datasetDB, err := db.OpenDatasetSQLite(absPath, true)
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
			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				sourceRoot, _ = ds.Metadata["shard_root"].(string)
			}
			if sourceRoot == "" {
				sourceRoot = ds.CurrentPath
			}
			compress, _ := ds.Metadata["compress"].(string)
			format, _ := ds.Metadata["format"].(string)
			if format == "" {
				format = "bin"
			}
			shardStore := shard.NewDatasetStore(datasetDB)
			shards, err := shardStore.FindByDataset(ctx, ds.ID)
			if err != nil {
				return errors.WrapE(err, "list dataset shards")
			}
			var total int
			for _, sh := range shards {
				n, err := shard.UnpackShardFile(ctx, shardStore, sh.ID,
					filepath.Join(sourceRoot, sh.FilePath), targetRoot, compress, format)
				if err != nil {
					return errors.WrapE(err, "unpack shard", "path", sh.FilePath)
				}
				total += n
			}
			fmt.Fprintf(os.Stderr, "unpacked %d files from dataset %s (%d shards)\n", total, ds.Name, len(shards))
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"dataset_uid": ds.UID,
				"files":       total,
				"shards":      len(shards),
			})
		},
	}
	cmd.Flags().String("dataset-db", "", "path to dataset SQLite")
	cmd.Flags().String("source-root", "", "root directory containing DATA shards")
	cmd.Flags().String("target-root", "", "target root directory for extracted files")
	return cmd
}
