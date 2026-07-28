package arcset

import (
	"context"
	"fmt"
	"os"

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
			ecStr, _ := cmd.Flags().GetString("ec")
			tapeMaxBytes, _ := cmd.Flags().GetInt64("tape-max-bytes")
			tapeCount, _ := cmd.Flags().GetInt("tape-count")

			metadata := map[string]any{}
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
			if tapeMaxBytes > 0 {
				metadata["tape_max_bytes"] = tapeMaxBytes
			}
			if tapeCount > 0 {
				metadata["tape_count"] = tapeCount
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
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, `{"arcset_id":%d}`+"\n", a.ID)
			return nil
		},
	}
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("target-root", "", "output directory for shard files")
	cmd.Flags().String("ec", "", "erasure code config (k+m, e.g. 8+4)")
	cmd.Flags().Int64("tape-max-bytes", 0, "max bytes per tape")
	cmd.Flags().Int("tape-count", 0, "total tape count (= k+m)")
	return cmd
}
