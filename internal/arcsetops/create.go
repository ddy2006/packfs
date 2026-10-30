package arcsetops

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateOptions controls arcset creation.
type CreateOptions struct {
	Name              string
	TargetRoot        string
	EC                string
	Groups            int
	Layout            string
	PartitionWidth    int
	SeqWidth          int
	PartitionCapacity int64
	ShardMaxBytes     int64
	Label             string
	Comment           string
}

// CreateResult is the outcome of arcset creation.
type CreateResult struct {
	Arcset         *arcset.Arcset
	PartitionCount int
}

// Create defines a new arcset in PostgreSQL.
func Create(ctx context.Context, pool *pgxpool.Pool, opts CreateOptions) (*CreateResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if opts.TargetRoot == "" {
		return nil, fmt.Errorf("target root is required")
	}
	if opts.EC == "" {
		return nil, fmt.Errorf("ec is required")
	}
	ecCfg, err := ec.ParseConfig(opts.EC)
	if err != nil {
		return nil, fmt.Errorf("invalid ec: %w", err)
	}
	if err := ecCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ec: %w", err)
	}
	if opts.Groups <= 0 {
		return nil, fmt.Errorf("groups must be positive")
	}
	if opts.Layout == "" {
		opts.Layout = "raid3"
	}
	if opts.Layout != "raid3" && opts.Layout != "raid5" {
		return nil, fmt.Errorf("layout must be raid3 or raid5")
	}
	if ecCfg.M == 0 && opts.Layout != "raid3" {
		return nil, fmt.Errorf("layout must be raid3 when m=0")
	}
	if opts.PartitionWidth == 0 {
		opts.PartitionWidth = 2
	}
	if opts.PartitionWidth < 1 || opts.PartitionWidth > 3 {
		return nil, fmt.Errorf("partition width must be 1..3")
	}
	if opts.SeqWidth == 0 {
		opts.SeqWidth = 3
	}
	if opts.SeqWidth < 1 || opts.SeqWidth > 5 {
		return nil, fmt.Errorf("seq width must be 1..5")
	}
	if opts.PartitionCapacity <= 0 {
		return nil, fmt.Errorf("partition capacity must be positive")
	}
	partitionCount := opts.Groups * ecCfg.Total()
	if partitionCount > int(math.Pow10(opts.PartitionWidth))-1 {
		return nil, fmt.Errorf("partition_count %d exceeds %d-digit partition width",
			partitionCount, opts.PartitionWidth)
	}

	metadata := map[string]any{
		"ec":                 fmt.Sprintf("%d+%d", ecCfg.K, ecCfg.M),
		"group_count":        opts.Groups,
		"layout":             opts.Layout,
		"partition_width":    opts.PartitionWidth,
		"seq_width":          opts.SeqWidth,
		"partition_capacity": opts.PartitionCapacity,
		"data_capacity":      int64(ecCfg.K) * int64(opts.Groups) * opts.PartitionCapacity,
		"raw_capacity":       int64(ecCfg.Total()) * int64(opts.Groups) * opts.PartitionCapacity,
		"create_time":        time.Now().Format(time.RFC3339),
	}
	if opts.ShardMaxBytes > 0 {
		metadata["shard_max_bytes"] = opts.ShardMaxBytes
	}

	store := arcset.NewPGStore(pool)
	a, err := store.Create(ctx, arcset.CreateParams{
		Name:        opts.Name,
		Label:       opts.Label,
		CurrentPath: opts.TargetRoot,
		Metadata:    metadata,
		Comment:     opts.Comment,
	})
	if err != nil {
		return nil, err
	}
	return &CreateResult{Arcset: a, PartitionCount: partitionCount}, nil
}
