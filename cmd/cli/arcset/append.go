package arcset

import (
	"context"
	"encoding/json"
	"os"

	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func appendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a dataset to an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			datasetDBPath, _ := cmd.Flags().GetString("dataset-db")
			if datasetDBPath == "" {
				return errors.NewUsage("--dataset-db is required")
			}
			datasetKey, _ := cmd.Flags().GetString("dataset-key")

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()

			result, err := arcsetops.Append(ctx, pool, arcsetops.AppendOptions{
				ArcsetID:   id,
				DatasetDB:  datasetDBPath,
				DatasetKey: datasetKey,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"arcset_id":    id,
				"dataset_uid":  result.Dataset.UID,
				"dataset_key":  result.DatasetKey,
				"append_order": result.Relation.AppendOrder,
			})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("dataset-db", "", "path to dataset SQLite")
	cmd.Flags().String("dataset-key", "", "dataset key within the arcset")
	return cmd
}
