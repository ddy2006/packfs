package arcset

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.NewUsage("--name is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
			}
			dsIDsStr, _ := cmd.Flags().GetString("dataset-ids")
			if dsIDsStr == "" {
				return errors.NewUsage("--dataset-ids is required")
			}

			dsIDs, err := parseIntList(dsIDsStr)
			if err != nil {
				return errors.WrapE(err, "parse dataset-ids")
			}
			if len(dsIDs) == 0 {
				return errors.NewUsage("--dataset-ids must contain at least one ID")
			}

			format, _ := cmd.Flags().GetString("format")
			if format == "" {
				format = "bin"
			}
			shardMaxBytes, _ := cmd.Flags().GetInt64("shard-max-bytes")
			compress, _ := cmd.Flags().GetString("compress")
			ecStr, _ := cmd.Flags().GetString("ec")

			metadata := map[string]any{
				"format": format,
			}
			if shardMaxBytes > 0 {
				metadata["shard_max_bytes"] = shardMaxBytes
			}
			if compress != "" {
				metadata["compress"] = compress
			}
			if ecStr != "" {
				ecCfg, err := ec.ParseConfig(ecStr)
				if err != nil {
					return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
				}
				if err := ecCfg.Validate(); err != nil {
					return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
				}
				metadata["ec"] = ecStr
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := arcset.NewSQLiteStore(sqlDB)
			a, err := arcset.CreateArcset(context.Background(), store, arcset.CreateArcsetParams{
				Name:        name,
				CurrentPath: targetRoot,
				Metadata:    metadata,
				DatasetIDs:  dsIDs,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, `{"arcset_id":%d}`+"\n", a.ID)
			return nil
		},
	}
	cmd.Flags().String("target-root", "", "output directory for shard files")
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("dataset-ids", "", "comma-separated dataset IDs")
	cmd.Flags().String("format", "bin", "pack format: bin/iso/tar")
	cmd.Flags().Int64("shard-max-bytes", 0, "max shard size in bytes")
	cmd.Flags().String("compress", "", "compression: zstd, segment:zstd, segment:xz, zstd_seekable, xz")
	cmd.Flags().String("ec", "", "erasure code config (k+m, e.g. 8+4)")
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
