package fuse

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BuildArcsetIndex builds a virtual index from PG placement plus each
// dataset SQLite. The virtual tree root contains one directory per dataset_key.
func BuildArcsetIndex(ctx context.Context, pool *pgxpool.Pool, arcsetID int64) (*Index, error) {
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, arcsetID)
	if err != nil {
		return nil, fmt.Errorf("find arcset: %w", err)
	}
	relations, err := store.ListRelations(ctx, arcsetID)
	if err != nil {
		return nil, err
	}
	shards, err := store.ListShards(ctx, arcsetID)
	if err != nil {
		return nil, err
	}
	dataPath := map[string]string{}
	for _, sh := range shards {
		if sh.Type != "DATA" || sh.DatasetRef == nil || sh.Seq == nil {
			continue
		}
		dataPath[fmt.Sprintf("%d/%d", *sh.DatasetRef, *sh.Seq)] = sh.FilePath
	}

	idx := &Index{Files: map[string][]SegmentLoc{}}
	idx.Compress, _ = a.Metadata["compress"].(string)
	for _, rel := range relations {
		dsDB, err := db.OpenDatasetSQLite(rel.SQLitePath, true)
		if err != nil {
			return nil, fmt.Errorf("open dataset %s: %w", rel.UID, err)
		}
		ds, err := findDataset(ctx, dsDB)
		if err != nil {
			dsDB.Close()
			return nil, fmt.Errorf("find dataset %s: %w", rel.UID, err)
		}
		if idx.Compress == "" {
			idx.Compress, _ = ds.Metadata["compress"].(string)
		}
		rows, err := dsDB.QueryContext(ctx, `
			SELECT f.file_path, f.file_size, COALESCE(seg.offset,0),
			       COALESCE(seg.size,0), COALESCE(seg.csize,0), s.seq
			FROM t_file f
			JOIN t_segment seg ON seg.file = f.id
			JOIN t_shard s ON s.id = seg.shard
			WHERE f.dataset = ?
			ORDER BY f.file_path, seg.offset`, ds.ID)
		if err != nil {
			dsDB.Close()
			return nil, fmt.Errorf("query dataset %s: %w", rel.UID, err)
		}
		for rows.Next() {
			var fp string
			var fileSize, offset, size, csize int64
			var seq int
			if err := rows.Scan(&fp, &fileSize, &offset, &size, &csize, &seq); err != nil {
				rows.Close()
				dsDB.Close()
				return nil, err
			}
			relPath := dataPath[fmt.Sprintf("%d/%d", rel.DatasetRef, seq)]
			shardPath := ""
			if relPath != "" {
				shardPath = filepath.Join(a.CurrentPath, relPath)
			}
			virtual := filepath.ToSlash(filepath.Join(rel.DatasetKey, fp))
			idx.Files[virtual] = append(idx.Files[virtual], SegmentLoc{
				ShardPath: shardPath,
				Offset:    offset,
				Size:      size,
				Csize:     csize,
				FileSize:  fileSize,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			dsDB.Close()
			return nil, err
		}
		rows.Close()
		dsDB.Close()
	}
	return idx, nil
}

func findDataset(ctx context.Context, db *sql.DB) (*dataset.Dataset, error) {
	store := dataset.NewDatasetStore(db)
	limit := 1
	all, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("dataset sqlite has no dataset")
	}
	return all[0], nil
}

// MountArcset mounts an arcset read-only. Root children are dataset_key dirs.
func MountArcset(ctx context.Context, pool *pgxpool.Pool, arcsetID int64, mountPoint string) (*fuse.Server, error) {
	idx, err := BuildArcsetIndex(ctx, pool, arcsetID)
	if err != nil {
		return nil, err
	}
	if len(idx.Files) == 0 {
		return nil, fmt.Errorf("no files found for arcset %d", arcsetID)
	}
	return mountIndex(idx, mountPoint)
}
