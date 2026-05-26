package shard

import "context"

// Store defines the persistence interface for shards.
type Store interface {
	CreateShard(ctx context.Context, s *Shard) error
	AddSegment(ctx context.Context, seg *Segment) error
	FindByArcset(ctx context.Context, arcsetID int) ([]*Shard, error)
}
