package arcset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/exec"
	"github.com/spf13/cobra"
)

func genDefCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-def",
		Short: "Generate shard-def files",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
			}
			script, _ := cmd.Flags().GetString("script")

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := arcset.NewSQLiteStore(sqlDB)
			a, err := store.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			var shardCount int64

			if script != "" {
				// 脚本模式
				cmdStr := fmt.Sprintf("%s --id %d --target-root %s", script, id, targetRoot)
				stdout, stderr, err := exec.RunReturnAll(cmdStr, 0)
				if stderr != "" {
					fmt.Print(stderr)
				}
				if err != nil {
					return errors.WrapE(err, "run gen script")
				}

				n, err := strconv.ParseInt(strings.TrimSpace(stdout), 10, 64)
				if err != nil {
					return errors.WrapE(err, "parse shard count from script output", "output", stdout)
				}
				shardCount = n
			} else {
				// 内置模式
				shards, err := arcset.GenerateShardDefs(context.Background(), store, a.ID)
				if err != nil {
					return errors.WrapE(err, "generate shard defs")
				}

				compressExt := compressExt(a.Metadata)
				for _, sd := range shards {
					fileName := fmt.Sprintf("%04d.%s.def", sd.Seq, compressExt)
					if err := writeDefFile(targetRoot, fileName, a.ID, sd.DatasetID, sd.Segments); err != nil {
						return errors.WrapE(err, "write def file")
					}
				}
				shardCount = int64(len(shards))
			}

			// EC 补齐：data shard 数必须能被 k 整除。
			shardCount = padForEC(a, store, targetRoot, shardCount)

			// 回写 shard_count
			if a.Metadata == nil {
				a.Metadata = make(map[string]any)
			}
			a.Metadata["shard_count"] = shardCount
			if err := store.Update(context.Background(), a.Name,
				arcset.Update{Metadata: a.Metadata}); err != nil {
				return errors.WrapE(err, "update shard_count")
			}

			fmt.Printf("generated %d shard-def file(s) for arcset %s in %s\n", shardCount, a.Name, targetRoot)
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	cmd.Flags().String("target-root", "", "target root directory for shard-def files")
	cmd.Flags().String("script", "", "external script for custom grouping")
	return cmd
}

// padForEC adds empty .def files so the total data shard count is a multiple of k.
// Returns the updated shard count.
func padForEC(a *arcset.Arcset, store arcset.Store, targetRoot string, shardCount int64) int64 {
	ecStr, ok := a.Metadata["ec"].(string)
	if !ok || ecStr == "" {
		return shardCount
	}
	ecCfg, err := ec.ParseConfig(ecStr)
	if err != nil {
		return shardCount
	}

	pad := int64(ecCfg.K) - (shardCount % int64(ecCfg.K))
	if pad <= 0 || pad >= int64(ecCfg.K) {
		return shardCount
	}

	dsID := firstDatasetID(store, a.ID)
	ext := compressExt(a.Metadata)
	for i := int64(0); i < pad; i++ {
		name := fmt.Sprintf("PAD_%04d.%s.def", i, ext)
		writeDefFile(targetRoot, name, a.ID, dsID, nil)
	}
	fmt.Printf("padded %d empty shard-def(s) for EC alignment (k=%d)\n", pad, ecCfg.K)
	return shardCount + pad
}

// firstDatasetID returns the ID of the first dataset linked to the arcset.
func firstDatasetID(store arcset.Store, arcsetID int) int {
	refs, err := store.ListDatasetRefs(context.Background(), arcsetID)
	if err != nil || len(refs) == 0 {
		return 0
	}
	return refs[0].ID
}

func writeDefFile(dir, fileName string, arcsetID, datasetID int, descs []arcset.SegmentDesc) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, fileName)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# arcset_id: %d\n", arcsetID)
	fmt.Fprintf(f, "# dataset_id: %d\n", datasetID)
	for _, d := range descs {
		if d.FileOffset == 0 && d.SegmentSize == d.FileSize {
			fmt.Fprintln(f, d.FilePath)
		} else {
			fmt.Fprintf(f, `{"path":"%s","offset":%d,"size":%d}`+"\n",
				d.FilePath, d.FileOffset, d.SegmentSize)
		}
	}
	return nil
}

func compressExt(metadata map[string]any) string {
	format, _ := metadata["format"].(string)
	if format == "" {
		format = "bin"
	}
	c, _ := metadata["compress"].(string)
	switch c {
	case "zstd", "xz", "zstd_seekable":
		return format + "." + algoExt(c)
	case "segment:zstd", "segment:xz":
		return algoExt(c) + "." + format
	default:
		return format
	}
}

func algoExt(compress string) string {
	switch compress {
	case "segment:zstd", "zstd", "zstd_seekable":
		return "zst"
	case "segment:xz", "xz":
		return "xz"
	default:
		return ""
	}
}
