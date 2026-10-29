package arcset

import (
	"context"
	"encoding/json"
	"os"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/validate"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate arcset DATA and EC shards",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			stage, _ := cmd.Flags().GetString("stage")
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			if err := validate.Run(ctx, pool, validate.Options{ArcsetID: id, Stage: stage}); err != nil {
				return errors.WrapE(err, "validate")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "stage": stage})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("stage", "all", "validate stage: data | ec | all")
	return cmd
}
