package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, _ := cmd.Flags().GetString("root-dir")
			if rootDir == "" {
				return errors.NewUsage("--root-dir is required")
			}
			name, _ := cmd.Flags().GetString("name")
			format, _ := cmd.Flags().GetString("format")
			compress, _ := cmd.Flags().GetString("compress")
			shardMaxBytes, _ := cmd.Flags().GetInt64("shard-max-bytes")
			genOnly, _ := cmd.Flags().GetBool("gen-only")
			dbDir, _ := cmd.Flags().GetString("db-dir")
			targetRoot, _ := cmd.Flags().GetString("target-root")
			sortRegex, _ := cmd.Flags().GetString("shard-sort-regex")
			sortIndex, _ := cmd.Flags().GetString("shard-sort-index")

			result, err := datasetops.Create(context.Background(), datasetops.CreateOptions{
				RootDir:        rootDir,
				Name:           name,
				DBDir:          resolveDBDir(dbDir),
				TargetRoot:     targetRoot,
				Format:         format,
				Compress:       compress,
				ShardMaxBytes:  shardMaxBytes,
				ShardSortRegex: sortRegex,
				ShardSortIndex: sortIndex,
				GenOnly:        genOnly,
			})
			if err != nil {
				return err
			}

			if genOnly {
				fmt.Fprintf(os.Stderr, "created dataset %s (scan only, no shard generation)\n", result.Dataset.Name)
			} else {
				fmt.Fprintf(os.Stderr, "created dataset %s (%d shards)\n",
					result.Dataset.Name, result.ShardCount)
			}
			return writeCreateResult(result.Dataset, result.SQLitePath, result.ShardCount)
		},
	}
	cmd.Flags().String("root-dir", "", "root directory to scan recursively")
	cmd.Flags().String("name", "", "dataset name (default: last component of root-dir)")
	cmd.Flags().String("db-dir", "", "directory for per-dataset SQLite files")
	cmd.Flags().String("target-root", "", "output directory for DATA shards")
	cmd.Flags().String("format", "bin", "shard format: bin / tar / iso")
	cmd.Flags().String("compress", "", "compression: zstd / xz / segment:zstd / segment:xz")
	cmd.Flags().Int64("shard-max-bytes", 0, "max shard size in bytes (0=unlimited)")
	cmd.Flags().String("shard-sort-regex", "", "regex extracting shard sort keys")
	cmd.Flags().String("shard-sort-index", "", "comma-separated capture group indexes")
	cmd.Flags().Bool("gen-only", false, "scan only, skip shard generation")
	return cmd
}

func writeCreateResult(ds *dataset.Dataset, sqlitePath string, shardCount int) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"dataset_uid":  ds.UID,
		"sqlite_path":  sqlitePath,
		"shard_count":  shardCount,
		"dataset_name": ds.Name,
	})
}
