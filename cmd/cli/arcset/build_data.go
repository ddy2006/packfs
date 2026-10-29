package arcset

import (
	"context"
	"encoding/json"
	"os"

	"github.com/ddy2006/packfs/internal/builddata"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func buildDataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build-data",
		Short: "Place sorted DATA shards into arcset partitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			datasetUID, _ := cmd.Flags().GetString("dataset-uid")
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()

			n, err := builddata.BuildData(ctx, pool, builddata.Options{
				ArcsetID:   id,
				DatasetUID: datasetUID,
			})
			if err != nil {
				return errors.WrapE(err, "build-data")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"built": n})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("dataset-uid", "", "only build one dataset by uid")
	return cmd
}
