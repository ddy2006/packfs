package arcset

import (
	"context"
	"fmt"
	"sort"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func showCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show arcset details",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			store := arcset.NewPGStore(pool)

			a, err := store.FindByID(ctx, id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}
			refs, err := store.ListRelations(ctx, id)
			if err != nil {
				return errors.WrapE(err, "list dataset relations")
			}
			shards, err := store.ListShards(ctx, id)
			if err != nil {
				return errors.WrapE(err, "list shards")
			}

			ecCfg, err := ec.ParseConfig(metaString(a.Metadata, "ec"))
			if err != nil {
				return errors.WrapE(err, "parse ec metadata")
			}
			groups := int(metaInt64(a.Metadata, "group_count"))
			if groups <= 0 {
				groups = 1
			}
			partitionCapacity := metaInt64(a.Metadata, "partition_capacity")
			dataCapacity := metaInt64(a.Metadata, "data_capacity")
			rawCapacity := metaInt64(a.Metadata, "raw_capacity")
			if dataCapacity == 0 {
				dataCapacity = int64(ecCfg.K) * int64(groups) * partitionCapacity
			}
			if rawCapacity == 0 {
				rawCapacity = int64(ecCfg.Total()) * int64(groups) * partitionCapacity
			}

			fmt.Printf("Arcset: %s (id=%d)\n", a.Name, a.ID)
			fmt.Printf("  Status: %s/%s  Health: %s  ParityDegraded: %t\n",
				a.Status, a.BuildPhase, a.Health, a.ParityDegraded)
			fmt.Printf("  Path: %s\n", a.CurrentPath)
			if a.Label != "" {
				fmt.Printf("  Label: %s\n", a.Label)
			}
			if a.Comment != "" {
				fmt.Printf("  Comment: %s\n", a.Comment)
			}
			if !a.LastCheck.IsZero() {
				fmt.Printf("  LastCheck: %s\n", a.LastCheck.Format("2006-01-02 15:04:05"))
			}
			fmt.Printf("  EC: k=%d m=%d  Groups: %d  Partitions: %d\n",
				ecCfg.K, ecCfg.M, groups, groups*ecCfg.Total())
			fmt.Printf("  Capacity: data=%s raw=%s per_partition=%s\n",
				humanBytes(dataCapacity), humanBytes(rawCapacity), humanBytes(partitionCapacity))

			fmt.Printf("Metadata:\n")
			for _, key := range sortedKeys(a.Metadata) {
				fmt.Printf("  %s=%v\n", key, a.Metadata[key])
			}

			fmt.Printf("Datasets (%d):\n", len(refs))
			for _, ref := range refs {
				fmt.Printf("  order=%d key=%s uid=%s name=%s sqlite=%s\n",
					ref.AppendOrder, ref.DatasetKey, ref.UID, ref.Name, ref.SQLitePath)
			}
			if len(refs) == 0 {
				fmt.Println("  (none)")
			}

			printShardStats(ecCfg, shards, partitionCapacity)
			return nil
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	return cmd
}

type shardStat struct {
	data, ec, pad int
	usedBytes     int64
}

func printShardStats(ecCfg ec.Config, shards []*arcset.ShardRow, partitionCapacity int64) {
	var dataCount, ecCount, padCount int
	partitions := map[int]*shardStat{}
	stripes := map[[2]int]*shardStat{}
	var totalUsed int64

	for _, sh := range shards {
		switch sh.Type {
		case "DATA":
			dataCount++
		case "EC":
			ecCount++
		case "PAD":
			padCount++
		}
		totalUsed += sh.FileSize

		if sh.Partition != nil {
			st := partitions[*sh.Partition]
			if st == nil {
				st = &shardStat{}
				partitions[*sh.Partition] = st
			}
			st.usedBytes += sh.FileSize
			switch sh.Type {
			case "DATA":
				st.data++
			case "EC":
				st.ec++
			case "PAD":
				st.pad++
			}
		}
		if sh.GroupNo != nil && sh.StripeSeq != nil {
			key := [2]int{*sh.GroupNo, *sh.StripeSeq}
			st := stripes[key]
			if st == nil {
				st = &shardStat{}
				stripes[key] = st
			}
			switch sh.Type {
			case "DATA":
				st.data++
			case "EC":
				st.ec++
			case "PAD":
				st.pad++
			}
		}
	}

	dataComplete, stripeComplete := 0, 0
	for _, st := range stripes {
		if st.data == ecCfg.K {
			dataComplete++
		}
		// A finished stripe has every data position (DATA or PAD) and all EC.
		if st.data+st.pad == ecCfg.K && (ecCfg.M == 0 || st.ec == ecCfg.M) {
			stripeComplete++
		}
	}

	fmt.Printf("Shards: DATA=%d EC=%d PAD=%d  Used=%s\n",
		dataCount, ecCount, padCount, humanBytes(totalUsed))
	fmt.Printf("Stripes: total=%d data_complete=%d ec_complete=%d\n",
		len(stripes), dataComplete, stripeComplete)

	partKeys := make([]int, 0, len(partitions))
	for part := range partitions {
		partKeys = append(partKeys, part)
	}
	sort.Ints(partKeys)
	fmt.Printf("Partitions used (%d):\n", len(partKeys))
	for _, part := range partKeys {
		st := partitions[part]
		usage := ""
		if partitionCapacity > 0 {
			usage = fmt.Sprintf(" (%.1f%%)", float64(st.usedBytes)*100/float64(partitionCapacity))
		}
		fmt.Printf("  partition=%d used=%s/%s%s DATA=%d EC=%d PAD=%d\n",
			part, humanBytes(st.usedBytes), humanBytes(partitionCapacity), usage,
			st.data, st.ec, st.pad)
	}
	if len(partKeys) == 0 {
		fmt.Println("  (none)")
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func metaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0"
	}
	const unit = 1024
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(n)
	i := 0
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}
