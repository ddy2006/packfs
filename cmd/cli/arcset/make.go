package arcset

import (
	"context"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func makeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Make arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.E("--name is required")
			}
			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				return errors.E("--source-root is required")
			}
			dsIDsStr, _ := cmd.Flags().GetString("dataset-ids")
			if dsIDsStr == "" {
				return errors.E("--dataset-ids is required")
			}

			dsIDs, err := parseIntList(dsIDsStr)
			if err != nil {
				return errors.WrapE(err, "parse dataset-ids")
			}
			if len(dsIDs) == 0 {
				return errors.E("--dataset-ids must contain at least one ID")
			}

			backend, _ := cmd.Flags().GetString("backend")
			if backend == "" {
				backend = "local"
			}
			segmentBytes, _ := cmd.Flags().GetInt64("segment-bytes")
			comment, _ := cmd.Flags().GetString("comment")

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := arcset.NewSQLiteStore(sqlDB)
			return arcset.CreateArcset(context.Background(), store, arcset.CreateArcsetParams{
				Name:         name,
				PathRegex:    sourceRoot,
				Backend:      backend,
				SegmentBytes: segmentBytes,
				DatasetIDs:   dsIDs,
				Comment:      comment,
			})
		},
	}
	cmd.Flags().String("source-root", "", "source root directory")
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("dataset-ids", "", "comma-separated dataset IDs")
	cmd.Flags().String("backend", "local", "storage backend")
	cmd.Flags().Int64("segment-bytes", 0, "segment size in bytes")
	cmd.Flags().String("comment", "", "comment")
	return cmd
}

func parseIntList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	var ids []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n)
	}
	return ids, nil
}
