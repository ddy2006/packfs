package dataset

import (
	"context"
	"fmt"
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
		Short: "Unpack dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			dsStore := dataset.NewSQLiteStore(sqlDB)
			ds, err := dsStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find dataset")
			}

			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				sourceRoot = ds.CurrentPath
			}

			shardStore := shard.NewSQLiteStore(sqlDB)
			shards, err := shardStore.FindByDataset(context.Background(), ds.ID)
			if err != nil {
				return errors.WrapE(err, "find shards")
			}

			compress, _ := ds.Metadata["compress"].(string)
			format, _ := ds.Metadata["format"].(string)
			if format == "" {
				format = "bin"
			}

			var totalFiles int
			for _, sh := range shards {
				shardAbsPath := filepath.Join(sourceRoot, sh.FilePath)
				count, err := shard.UnpackShardFile(context.Background(), shardStore, sh.ID, shardAbsPath, targetRoot, compress, format)
				if err != nil {
					return errors.WrapE(err, "unpack shard", "path", sh.FilePath)
				}
				totalFiles += count
			}

			fmt.Printf("unpacked %d files from dataset %s (%d shards)\n", totalFiles, ds.Name, len(shards))
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "dataset ID")
	cmd.Flags().String("source-root", "", "root directory where shard files reside (default: dataset current_path)")
	cmd.Flags().String("target-root", "", "target root directory for extracted files")
	return cmd
}
