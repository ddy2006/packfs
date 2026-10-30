package arcset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/db"
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
			if ecStr == "" {
				return errors.NewUsage("--ec is required")
			}
			groupCount, _ := cmd.Flags().GetInt("groups")
			layout, _ := cmd.Flags().GetString("layout")
			partitionWidth, _ := cmd.Flags().GetInt("partition-width")
			seqWidth, _ := cmd.Flags().GetInt("seq-width")
			capacityStr, _ := cmd.Flags().GetString("partition-capacity")
			capacity, err := arcsetops.ParseBytes(capacityStr)
			if err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --partition-capacity: %v", err))
			}
			shardMaxBytes, _ := cmd.Flags().GetInt64("shard-max-bytes")
			label, _ := cmd.Flags().GetString("label")
			comment, _ := cmd.Flags().GetString("comment")

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()

			result, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
				Name:              name,
				TargetRoot:        targetRoot,
				EC:                ecStr,
				Groups:            groupCount,
				Layout:            layout,
				PartitionWidth:    partitionWidth,
				SeqWidth:          seqWidth,
				PartitionCapacity: capacity,
				ShardMaxBytes:     shardMaxBytes,
				Label:             label,
				Comment:           comment,
			})
			if err != nil {
				return errors.WrapE(err, "create arcset")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"arcset_id":       result.Arcset.ID,
				"status":          result.Arcset.Status,
				"partition_count": result.PartitionCount,
			})
		},
	}
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("target-root", "", "output directory for shard files")
	cmd.Flags().String("ec", "", "erasure code config: k or k+m (e.g. 8 or 8+4)")
	cmd.Flags().Int("groups", 1, "partition group count")
	cmd.Flags().String("layout", "raid3", "partition layout: raid3 or raid5")
	cmd.Flags().Int("partition-width", 2, "partition directory width (1..3)")
	cmd.Flags().Int("seq-width", 3, "stripe seq width (1..5)")
	cmd.Flags().String("partition-capacity", "", "capacity per partition, e.g. 18TB")
	cmd.Flags().Int64("shard-max-bytes", 0, "max DATA shard size in bytes")
	cmd.Flags().String("label", "", "arcset label")
	cmd.Flags().String("comment", "", "arcset comment")
	return cmd
}
