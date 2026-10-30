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

func expandCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "expand",
		Short: "Add partition groups to an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			addGroups, _ := cmd.Flags().GetInt("add-groups")
			if addGroups <= 0 {
				return errors.NewUsage("--add-groups must be positive")
			}

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()

			result, err := arcsetops.Expand(ctx, pool, id, addGroups)
			if err != nil {
				return errors.WrapE(err, "expand")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"arcset_id":       id,
				"group_count":     result.GroupCount,
				"partition_count": result.PartitionCount,
				"data_capacity":   result.DataCapacity,
				"raw_capacity":    result.RawCapacity,
			})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().Int("add-groups", 0, "number of partition groups to add")
	return cmd
}
