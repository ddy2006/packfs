package arcset

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

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
			if ecStr == "" {
				return errors.NewUsage("--ec is required")
			}
			ecCfg, err := ec.ParseConfig(ecStr)
			if err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
			}
			if err := ecCfg.Validate(); err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
			}

			groupCount, _ := cmd.Flags().GetInt("groups")
			if groupCount <= 0 {
				return errors.NewUsage("--groups must be positive")
			}
			layout, _ := cmd.Flags().GetString("layout")
			if layout != "raid3" && layout != "raid5" {
				return errors.NewUsage("--layout must be raid3 or raid5")
			}
			if ecCfg.M == 0 && layout != "raid3" {
				return errors.NewUsage("--layout must be raid3 when m=0")
			}
			partitionWidth, _ := cmd.Flags().GetInt("partition-width")
			if partitionWidth < 1 || partitionWidth > 3 {
				return errors.NewUsage("--partition-width must be 1..3")
			}
			seqWidth, _ := cmd.Flags().GetInt("seq-width")
			if seqWidth < 1 || seqWidth > 5 {
				return errors.NewUsage("--seq-width must be 1..5")
			}
			capacityStr, _ := cmd.Flags().GetString("partition-capacity")
			capacity, err := parseBytes(capacityStr)
			if err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --partition-capacity: %v", err))
			}
			shardMaxBytes, _ := cmd.Flags().GetInt64("shard-max-bytes")
			label, _ := cmd.Flags().GetString("label")
			comment, _ := cmd.Flags().GetString("comment")

			partitionCount := groupCount * ecCfg.Total()
			if partitionCount > int(math.Pow10(partitionWidth))-1 {
				return errors.NewUsage(fmt.Sprintf("partition_count %d exceeds %d-digit partition width", partitionCount, partitionWidth))
			}

			metadata := map[string]any{
				"ec":                 fmt.Sprintf("%d+%d", ecCfg.K, ecCfg.M),
				"group_count":        groupCount,
				"layout":             layout,
				"partition_width":    partitionWidth,
				"seq_width":          seqWidth,
				"partition_capacity": capacity,
				"data_capacity":      int64(ecCfg.K) * int64(groupCount) * capacity,
				"raw_capacity":       int64(ecCfg.Total()) * int64(groupCount) * capacity,
				"create_time":        time.Now().Format(time.RFC3339),
			}
			if shardMaxBytes > 0 {
				metadata["shard_max_bytes"] = shardMaxBytes
			}

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			store := arcset.NewPGStore(pool)
			a, err := store.Create(ctx, arcset.CreateParams{
				Name:        name,
				Label:       label,
				CurrentPath: targetRoot,
				Metadata:    metadata,
				Comment:     comment,
			})
			if err != nil {
				return errors.WrapE(err, "create arcset")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"arcset_id":       a.ID,
				"status":          a.Status,
				"partition_count": partitionCount,
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

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("value is required")
	}
	multiplier := int64(1)
	upper := strings.ToUpper(s)
	for _, suffix := range []struct {
		name string
		mult int64
	}{
		{"PIB", 1 << 50}, {"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"PB", 1 << 50}, {"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"P", 1 << 50}, {"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
	} {
		if strings.HasSuffix(upper, suffix.name) {
			multiplier = suffix.mult
			s = strings.TrimSpace(s[:len(s)-len(suffix.name)])
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 || n > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("value out of range")
	}
	return n * multiplier, nil
}
