package shard

import "context"

// Store defines the persistence interface for shards.
type Store interface {
	CreateShard(ctx context.Context, s *Shard) error
	ReplaceSegments(ctx context.Context, shardID int, segs []*Segment) error
	FindByArcset(ctx context.Context, arcsetID int) ([]*Shard, error)
	FindByArcsetAndFilePath(ctx context.Context, arcsetID int, filePath string) (*Shard, error)
	ListUnpackInfo(ctx context.Context, shardID int) ([]UnpackInfo, error)
}
