package arcsetops

import (
	"context"
	"fmt"
	"math"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpandResult is the recalculated capacity after adding groups.
type ExpandResult struct {
	GroupCount     int
	PartitionCount int
	DataCapacity   int64
	RawCapacity    int64
}

// Expand adds partition groups to an arcset.
func Expand(ctx context.Context, pool *pgxpool.Pool, id int64, addGroups int) (*ExpandResult, error) {
	if addGroups <= 0 {
		return nil, fmt.Errorf("add-groups must be positive")
	}
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find arcset: %w", err)
	}
	switch a.Status {
	case "defined", "building", "completed":
	default:
		return nil, fmt.Errorf("arcset status does not allow expand: %s", a.Status)
	}
	if a.Health != "ok" {
		return nil, fmt.Errorf("arcset health is not ok: %s", a.Health)
	}
	ecStr, _ := a.Metadata["ec"].(string)
	ecCfg, err := ec.ParseConfig(ecStr)
	if err != nil {
		return nil, fmt.Errorf("parse ec metadata: %w", err)
	}
	if err := ecCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ec metadata: %w", err)
	}
	meta := a.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	groupCount := int(metaInt64(meta, "group_count"))
	if groupCount <= 0 {
		return nil, fmt.Errorf("arcset metadata missing group_count")
	}
	partitionWidth := int(metaInt64(meta, "partition_width"))
	if partitionWidth == 0 {
		partitionWidth = 2
	}
	partitionCapacity := metaInt64(meta, "partition_capacity")
	if partitionCapacity <= 0 {
		return nil, fmt.Errorf("arcset metadata missing partition_capacity")
	}

	newGroups := groupCount + addGroups
	partitionCount := newGroups * ecCfg.Total()
	maxPartitions := int(math.Pow10(partitionWidth)) - 1
	if partitionCount > maxPartitions {
		return nil, fmt.Errorf("partition_count %d exceeds partition width %d (max %d)",
			partitionCount, partitionWidth, maxPartitions)
	}
	if partitionCapacity > math.MaxInt64/int64(ecCfg.K)/int64(newGroups) ||
		partitionCapacity > math.MaxInt64/int64(ecCfg.Total())/int64(newGroups) {
		return nil, fmt.Errorf("capacity overflow for partition_capacity=%d", partitionCapacity)
	}
	dataCapacity := int64(ecCfg.K) * int64(newGroups) * partitionCapacity
	rawCapacity := int64(ecCfg.Total()) * int64(newGroups) * partitionCapacity

	meta["group_count"] = newGroups
	meta["data_capacity"] = dataCapacity
	meta["raw_capacity"] = rawCapacity
	if err := store.UpdateMetadata(ctx, id, meta); err != nil {
		return nil, fmt.Errorf("update arcset metadata: %w", err)
	}
	return &ExpandResult{
		GroupCount:     newGroups,
		PartitionCount: partitionCount,
		DataCapacity:   dataCapacity,
		RawCapacity:    rawCapacity,
	}, nil
}
