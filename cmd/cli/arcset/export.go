package arcset

import (
	"context"
	"encoding/json"
	"os"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/export"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export arcset.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			path, err := export.Run(ctx, pool, export.Options{ArcsetID: id, TargetRoot: targetRoot})
			if err != nil {
				return errors.WrapE(err, "export")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"manifest": path})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("target-root", "", "export directory (default: arcset current_path)")
	return cmd
}
