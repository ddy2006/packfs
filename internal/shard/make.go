package shard

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaichao/gopkg/errors"
)

// MakeConfig holds configuration for making a shard file.
type MakeConfig struct {
	Format     string // bin / tar / iso
	Compress   string // empty / zstd / xz / segment:zstd / segment:xz
	SourceRoot string // root directory for source files
	ArcsetID   int    // FK, 0 if not part of an arcset
	DatasetID  int    // FK
	Seq        int    // dataset-local shard seq; -1 = allocate next
}

// MakeResult holds the result of making a shard.
type MakeResult struct {
	Path     string
	Size     int64
	Checksum string
	Segments int
}

// ShardWriter records a dataset-level shard and its segments.
type ShardWriter interface {
	CreateShard(ctx context.Context, s *Shard) error
	ReplaceSegments(ctx context.Context, shardID int, segs []*Segment) error
}

// MakeShard creates a single shard file from segment definitions using the
// legacy SQLite store.
func MakeShard(ctx context.Context, db *sql.DB, cfg MakeConfig, segs []SegmentDef, outputPath string) (*MakeResult, error) {
	return MakeShardWithWriter(ctx, db, NewSQLiteStore(db), cfg, segs, outputPath)
}

// MakeShardWithWriter creates a single shard file and records it through writer.
func MakeShardWithWriter(ctx context.Context, db *sql.DB, writer ShardWriter, cfg MakeConfig, segs []SegmentDef, outputPath string) (*MakeResult, error) {
	if cfg.Format == "" {
		cfg.Format = "bin"
	}
	switch cfg.Format {
	case "tar":
		return makeTar(ctx, db, writer, cfg, segs, outputPath)
	case "iso":
		return makeIso(ctx, db, writer, cfg, segs, outputPath)
	case "bin":
		return makeBin(ctx, db, writer, cfg, segs, outputPath)
	default:
		return nil, fmt.Errorf("unknown shard format: %s", cfg.Format)
	}
}

// resolveSegFileID queries t_file for the file ID of a segment's relative path.
func resolveSegFileID(ctx context.Context, db *sql.DB, filePath string, datasetID int) (int, int64) {
	var fileID int
	var dbSize int64
	_ = db.QueryRowContext(ctx,
		`SELECT id, file_size FROM t_file WHERE file_path = ? AND dataset = ?`,
		filePath, datasetID).Scan(&fileID, &dbSize)
	return fileID, dbSize
}

// parseCompressMode splits compress config into component flags.
func parseCompressMode(compress string) (isSegment, isShard, isXZ bool) {
	switch compress {
	case "segment:zstd", "segment:xz":
		isSegment = true
	case "zstd", "xz":
		isShard = true
	}
	isXZ = compress == "xz" || compress == "segment:xz"
	return
}

// writeShardRecord upserts a shard row and fills shardID.
func writeShardRecord(ctx context.Context, store ShardWriter, shardID *int, cfg MakeConfig, relPath string, fileSize int64, checksum string) error {
	sh := &Shard{
		Seq:      cfg.Seq,
		FilePath: relPath,
		FileSize: fileSize,
		Type:     "DATA",
		Checksum: checksum,
		Arcset:   sql.NullInt64{Int64: int64(cfg.ArcsetID), Valid: cfg.ArcsetID > 0},
		Dataset:  sql.NullInt64{Int64: int64(cfg.DatasetID), Valid: cfg.DatasetID > 0},
	}
	if err := store.CreateShard(ctx, sh); err != nil {
		return errors.WrapE(err, "create shard record")
	}
	*shardID = sh.ID
	return nil
}
