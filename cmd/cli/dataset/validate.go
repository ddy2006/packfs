package dataset

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate all shard checksums in dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
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

			var ok, fail int
			for _, sh := range shards {
				absPath := filepath.Join(sourceRoot, sh.FilePath)
				f, err := os.Open(absPath)
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", sh.FilePath, err)
					fail++
					continue
				}

				h := sha256.New()
				diskSize, err := io.Copy(h, f)
				f.Close()
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", sh.FilePath, err)
					fail++
					continue
				}

				diskSum := fmt.Sprintf("%x", h.Sum(nil))
				if diskSum != sh.Checksum {
					fmt.Printf("FAIL %s: checksum mismatch (db=%s, disk=%s)\n",
						sh.FilePath, sh.Checksum, diskSum)
					fail++
				} else {
					fmt.Printf("OK   %s (%d bytes)\n", sh.FilePath, diskSize)
					ok++
				}
			}

			fmt.Printf("\n%d ok, %d failed (total %d shards)\n", ok, fail, len(shards))
			if fail > 0 {
				return errors.E("validation failed", "ok", ok, "fail", fail)
			}

			// Update shard_count metadata
			if ds.Metadata == nil {
				ds.Metadata = make(map[string]any)
			}
			actual := len(shards)
			expected := int64(0)
			if v, ok := ds.Metadata["shard_count"]; ok {
				switch n := v.(type) {
				case float64:
					expected = int64(n)
				case int64:
					expected = n
				}
			}
			if expected > 0 && int64(actual) > expected {
				fmt.Printf("shard_count updated: %d → %d\n", expected, actual)
				ds.Metadata["shard_count"] = float64(actual)
				if err := dsStore.UpdateMetadata(context.Background(), ds.ID, ds.Metadata); err != nil {
					return errors.WrapE(err, "update shard_count")
				}
			}

			return nil
		},
	}
	cmd.Flags().Int("id", 0, "dataset ID")
	cmd.Flags().String("source-root", "", "root directory where shard files reside (default: dataset current_path)")
	return cmd
}
