package arcset

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate all shard checksums in arcset",
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

			arcStore := arcset.NewSQLiteStore(sqlDB)
			a, err := arcStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			shardStore := shard.NewSQLiteStore(sqlDB)
			shards, err := shardStore.FindByArcset(context.Background(), a.ID)
			if err != nil {
				return errors.WrapE(err, "find shards")
			}

			var ok, fail int
			for _, sh := range shards {
				absPath := filepath.Join(a.CurrentPath, sh.FilePath)
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

			// shard 数量检查
			meta := a.Metadata
			if meta == nil {
				meta = make(map[string]any)
			}
			expected := int64(0)
			if v, ok := meta["shard_count"]; ok {
				switch n := v.(type) {
				case float64:
					expected = int64(n)
				case int64:
					expected = n
				}
			}
			actual := int64(len(shards))
			if expected > 0 {
				if actual > expected {
					fmt.Printf("shard_count updated: %d → %d\n", expected, actual)
					meta["shard_count"] = actual
					if err := arcStore.Update(context.Background(), a.Name,
						arcset.Update{Metadata: meta}); err != nil {
						return errors.WrapE(err, "update shard_count")
					}
				} else if actual < expected {
					return errors.E("shard count below expected",
						"expected", expected, "actual", actual)
				}
			}

			// 标为 complete
			complete := "complete"
			if err := arcStore.Update(context.Background(), a.Name,
				arcset.Update{Status: &complete}); err != nil {
				return errors.WrapE(err, "update arcset status to complete")
			}

			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	return cmd
}
