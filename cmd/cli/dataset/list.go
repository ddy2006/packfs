package dataset

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"strings"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List datasets in a dataset directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbDir, _ := cmd.Flags().GetString("db-dir")
			dbDir = resolveDBDir(dbDir)
			nameFilter, _ := cmd.Flags().GetString("name")
			statusFilter, _ := cmd.Flags().GetString("status")

			infos, err := datasetops.Scan(context.Background(), dbDir)
			if err != nil && !stderrors.Is(err, os.ErrNotExist) {
				return errors.WrapE(err, "list datasets")
			}

			fmt.Println(strings.Join([]string{
				"UID", "NAME", "STATUS", "FILES", "BYTES", "SHARDS", "SQLITE_PATH",
			}, "\t"))
			count := 0
			for _, info := range infos {
				ds := info.Dataset
				if nameFilter != "" && !strings.Contains(ds.Name, nameFilter) {
					continue
				}
				if statusFilter != "" && ds.Status != statusFilter {
					continue
				}
				fmt.Printf("%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
					ds.UID, ds.Name, ds.Status,
					metaInt64(ds.Metadata, "num_files"),
					metaInt64(ds.Metadata, "total_bytes"),
					info.ShardCount, info.SQLitePath)
				count++
			}
			fmt.Printf("(%d datasets)\n", count)
			return nil
		},
	}
	cmd.Flags().String("db-dir", "", "directory containing per-dataset SQLite files")
	cmd.Flags().String("name", "", "filter by dataset name (substring match)")
	cmd.Flags().String("status", "", "filter by status: building / built")
	return cmd
}

func resolveDBDir(dbDir string) string {
	if dbDir != "" {
		return dbDir
	}
	return dataset.DefaultDBDir()
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
