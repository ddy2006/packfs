// Package datasetops implements dataset creation and lifecycle operations
// shared by the CLI and HTTP API.
package datasetops

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/shard"
)

// CreateOptions controls dataset creation.
type CreateOptions struct {
	RootDir        string
	Name           string
	DBDir          string
	TargetRoot     string
	Format         string
	Compress       string
	ShardMaxBytes  int64
	ShardSortRegex string
	ShardSortIndex string
	GenOnly        bool
}

// CreateResult is the outcome of dataset creation.
type CreateResult struct {
	Dataset    *dataset.Dataset
	SQLitePath string
	ShardCount int
}

// Info describes one dataset found in a dataset directory.
type Info struct {
	Dataset    *dataset.Dataset
	SQLitePath string
	ShardCount int
}

// Create scans a directory, creates a dataset SQLite, and optionally builds
// DATA shards.
func Create(ctx context.Context, opts CreateOptions) (*CreateResult, error) {
	if opts.RootDir == "" {
		return nil, fmt.Errorf("root directory is required")
	}
	dbDir := opts.DBDir
	if dbDir == "" {
		dbDir = dataset.DefaultDBDir()
	}
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("create dataset db directory: %w", err)
	}

	uid, err := dataset.NewUID()
	if err != nil {
		return nil, err
	}
	sqlitePath := dataset.DBPath(dbDir, uid)
	dsDB, err := db.OpenDatasetSQLite(sqlitePath, false)
	if err != nil {
		return nil, fmt.Errorf("open dataset sqlite: %w", err)
	}
	defer dsDB.Close()

	store := dataset.NewDatasetStore(dsDB)
	name := opts.Name
	if name == "" {
		name = filepath.Base(opts.RootDir)
	}
	ds, err := dataset.CreateFromDirWithOptions(ctx, store, opts.RootDir, name, dataset.CreateOptions{
		UID:    uid,
		Status: "building",
	})
	if err != nil {
		return nil, err
	}
	ds, err = store.FindByID(ctx, ds.ID)
	if err != nil {
		return nil, fmt.Errorf("re-fetch dataset: %w", err)
	}

	targetRoot := opts.TargetRoot
	if targetRoot == "" {
		targetRoot = ds.CurrentPath
	}
	if err := os.MkdirAll(targetRoot, 0755); err != nil {
		return nil, fmt.Errorf("create shard root: %w", err)
	}
	format := opts.Format
	if format == "" {
		format = "bin"
	}
	meta := ds.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["format"] = format
	meta["compress"] = opts.Compress
	meta["shard_max_bytes"] = opts.ShardMaxBytes
	meta["shard_root"] = targetRoot
	if opts.ShardSortRegex != "" {
		meta["shard_sort_regex"] = opts.ShardSortRegex
	}
	if opts.ShardSortIndex != "" {
		meta["shard_sort_index"] = opts.ShardSortIndex
	}
	if err := store.UpdateMetadata(ctx, ds.ID, meta); err != nil {
		return nil, fmt.Errorf("update dataset metadata: %w", err)
	}
	ds.Metadata = meta

	result := &CreateResult{Dataset: ds, SQLitePath: sqlitePath}
	if opts.GenOnly {
		return result, nil
	}
	count, err := buildShards(ctx, dsDB, store, ds)
	if err != nil {
		return nil, err
	}
	result.ShardCount = count
	return result, nil
}

// BuildShards builds DATA shards for an existing dataset created with
// --gen-only and marks it built.
func BuildShards(ctx context.Context, sqlitePath string) (*dataset.Dataset, int, error) {
	dsDB, err := db.OpenDatasetSQLite(sqlitePath, false)
	if err != nil {
		return nil, 0, fmt.Errorf("open dataset sqlite: %w", err)
	}
	defer dsDB.Close()

	store := dataset.NewDatasetStore(dsDB)
	ds, err := FindSingle(ctx, store)
	if err != nil {
		return nil, 0, err
	}
	if ds.Status == "built" {
		return nil, 0, fmt.Errorf("dataset is already built")
	}
	count, err := buildShards(ctx, dsDB, store, ds)
	if err != nil {
		return nil, 0, err
	}
	return ds, count, nil
}

// MarkBuilt validates all DATA shards and sets status=built.
func MarkBuilt(ctx context.Context, sqlitePath string) (*dataset.Dataset, int, error) {
	dsDB, err := db.OpenDatasetSQLite(sqlitePath, false)
	if err != nil {
		return nil, 0, fmt.Errorf("open dataset sqlite: %w", err)
	}
	defer dsDB.Close()

	store := dataset.NewDatasetStore(dsDB)
	ds, err := FindSingle(ctx, store)
	if err != nil {
		return nil, 0, err
	}
	shardStore := shard.NewDatasetStore(dsDB)
	shards, err := shardStore.FindByDataset(ctx, ds.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("list dataset shards: %w", err)
	}
	if len(shards) == 0 {
		return nil, 0, fmt.Errorf("dataset has no DATA shards")
	}
	root := metaString(ds.Metadata, "shard_root")
	if root == "" {
		root = ds.CurrentPath
	}
	for _, sh := range shards {
		sum, err := ec.SHA256File(filepath.Join(root, sh.FilePath))
		if err != nil {
			return nil, 0, fmt.Errorf("validate shard %s: %w", sh.FilePath, err)
		}
		if sh.Checksum != "" && sum != sh.Checksum {
			return nil, 0, fmt.Errorf("shard checksum mismatch: %s", sh.FilePath)
		}
	}
	if ds.Metadata == nil {
		ds.Metadata = map[string]any{}
	}
	ds.Metadata["shard_count"] = len(shards)
	if err := store.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
		return nil, 0, fmt.Errorf("update shard_count: %w", err)
	}
	if err := store.UpdateStatus(ctx, ds.ID, "built"); err != nil {
		return nil, 0, fmt.Errorf("mark dataset built: %w", err)
	}
	ds.Status = "built"
	return ds, len(shards), nil
}

// Scan lists datasets in a dataset directory.
func Scan(ctx context.Context, dbDir string) ([]Info, error) {
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		return nil, fmt.Errorf("read dataset db directory %s: %w", dbDir, err)
	}
	var out []Info
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		path := filepath.Join(dbDir, entry.Name())
		info, err := LoadInfo(ctx, path)
		if err != nil {
			return nil, err
		}
		out = append(out, info...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dataset.Name != out[j].Dataset.Name {
			return out[i].Dataset.Name < out[j].Dataset.Name
		}
		return out[i].Dataset.UID < out[j].Dataset.UID
	})
	return out, nil
}

// LoadInfo opens one dataset SQLite and returns its dataset and DATA shards.
func LoadInfo(ctx context.Context, sqlitePath string) ([]Info, error) {
	dsDB, err := db.OpenDatasetSQLite(sqlitePath, true)
	if err != nil {
		return nil, fmt.Errorf("open dataset sqlite %s: %w", sqlitePath, err)
	}
	defer dsDB.Close()

	store := dataset.NewDatasetStore(dsDB)
	datasets, err := store.Find(ctx, dataset.Filter{})
	if err != nil {
		return nil, fmt.Errorf("list datasets in %s: %w", sqlitePath, err)
	}
	shardStore := shard.NewDatasetStore(dsDB)
	out := make([]Info, 0, len(datasets))
	for _, ds := range datasets {
		shards, err := shardStore.FindByDataset(ctx, ds.ID)
		if err != nil {
			return nil, fmt.Errorf("list shards for dataset %s: %w", ds.UID, err)
		}
		out = append(out, Info{Dataset: ds, SQLitePath: sqlitePath, ShardCount: len(shards)})
	}
	return out, nil
}

// LoadInfoByUID opens <dbDir>/<uid>.db.
func LoadInfoByUID(ctx context.Context, dbDir, uid string) (*Info, error) {
	if err := dataset.ValidateUID(uid); err != nil {
		return nil, err
	}
	infos, err := LoadInfo(ctx, dataset.DBPath(dbDir, uid))
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("dataset %s not found", uid)
	}
	return &infos[0], nil
}

// FindSingle returns the only dataset row in a dataset SQLite.
func FindSingle(ctx context.Context, store *dataset.DatasetStore) (*dataset.Dataset, error) {
	limit := 1
	all, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		return nil, fmt.Errorf("list dataset: %w", err)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("dataset sqlite contains no dataset")
	}
	return all[0], nil
}

func buildShards(ctx context.Context, dsDB *sql.DB, store *dataset.DatasetStore, ds *dataset.Dataset) (int, error) {
	root := metaString(ds.Metadata, "shard_root")
	if root == "" {
		root = ds.CurrentPath
	}
	format := metaString(ds.Metadata, "format")
	if format == "" {
		format = "bin"
	}
	compress := metaString(ds.Metadata, "compress")
	shardMaxBytes := metaInt64(ds.Metadata, "shard_max_bytes")

	files, err := store.ListFiles(ctx, ds.ID)
	if err != nil {
		return 0, fmt.Errorf("list dataset files: %w", err)
	}
	defs := dataset.GenerateShardDefs(files, shardMaxBytes, ds.ID)
	shardStore := shard.NewDatasetStore(dsDB)
	for _, def := range defs {
		outName := fmt.Sprintf("%04d.%s", def.Seq, format)
		outPath := filepath.Join(root, outName)
		cfg := shard.MakeConfig{
			Format:     format,
			Compress:   compress,
			SourceRoot: ds.CurrentPath,
			DatasetID:  ds.ID,
			Seq:        def.Seq,
		}
		segs := make([]shard.SegmentDef, len(def.Segments))
		for i, seg := range def.Segments {
			segs[i] = shard.SegmentDef{
				Path:   seg.FilePath,
				Offset: seg.FileOffset,
				Size:   seg.SegmentSize,
				FileID: seg.FileID,
			}
		}
		if _, err := shard.MakeShardWithWriter(ctx, dsDB, shardStore, cfg, segs, outPath); err != nil {
			return 0, fmt.Errorf("make shard %d: %w", def.Seq, err)
		}
	}
	if ds.Metadata == nil {
		ds.Metadata = map[string]any{}
	}
	ds.Metadata["shard_count"] = len(defs)
	if err := store.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
		return 0, fmt.Errorf("update shard_count: %w", err)
	}
	if err := store.UpdateStatus(ctx, ds.ID, "built"); err != nil {
		return 0, fmt.Errorf("mark dataset built: %w", err)
	}
	ds.Status = "built"
	return len(defs), nil
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
