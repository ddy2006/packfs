package dataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func markBuiltCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-built",
		Short: "Validate DATA shards and mark a dataset built",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := cmd.Flags().GetString("dataset-db")
			if dbPath == "" {
				return errors.NewUsage("--dataset-db is required")
			}
			absPath, err := filepath.Abs(dbPath)
			if err != nil {
				return errors.WrapE(err, "resolve dataset sqlite path")
			}
			ds, count, err := datasetops.MarkBuilt(context.Background(), absPath)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"dataset_uid": ds.UID,
				"status":      "built",
				"shard_count": count,
			})
		},
	}
	cmd.Flags().String("dataset-db", "", "path to dataset SQLite")
	return cmd
}
