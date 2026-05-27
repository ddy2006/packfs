package shard_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/shard"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	schema := `
	CREATE TABLE t_shard (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		seq SMALLINT,
		file_path TEXT,
		file_size BIGINT,
		type VARCHAR,
		checksum VARCHAR,
		metadata JSON,
		last_check DATETIME,
		arcset INTEGER NOT NULL REFERENCES t_arcset(id) ON DELETE CASCADE
	);
	CREATE TABLE t_segment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		offset BIGINT,
		size BIGINT,
		shard INTEGER NOT NULL REFERENCES t_shard(id) ON DELETE CASCADE,
		arcset INTEGER NOT NULL,
		file INTEGER NOT NULL,
		file_offset BIGINT,
		file_size BIGINT
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestCreateShard(t *testing.T) {
	db := setupDB(t)
	store := shard.NewSQLiteStore(db)
	ctx := context.Background()

	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "a.txt")
	data := []byte("hello world, this is test data for shard packing")
	if err := os.WriteFile(srcFile, data, 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	descs := []arcset.SegmentDesc{
		{FilePath: srcFile, FileSize: int64(len(data)), FileOffset: 0, SegmentSize: int64(len(data)), FileID: 1},
	}

	outputDir := t.TempDir()
	err := shard.CreateShard(ctx, store, descs, 1, 0, outputDir, "DATA")
	if err != nil {
		t.Fatalf("CreateShard: %v", err)
	}

	shardFile := filepath.Join(outputDir, "shard_1_0000.pak")
	shardData, err := os.ReadFile(shardFile)
	if err != nil {
		t.Fatalf("read shard file: %v", err)
	}
	if string(shardData) != string(data) {
		t.Errorf("shard content mismatch: got %q, want %q", string(shardData), string(data))
	}

	shards, err := store.FindByArcset(ctx, 1)
	if err != nil {
		t.Fatalf("FindByArcset: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard, got %d", len(shards))
	}
	if shards[0].FileSize != int64(len(data)) {
		t.Errorf("FileSize: got %d, want %d", shards[0].FileSize, len(data))
	}
	if shards[0].Checksum == "" {
		t.Error("expected non-empty shard checksum")
	}
}

func TestCreateShardMultipleSegments(t *testing.T) {
	db := setupDB(t)
	store := shard.NewSQLiteStore(db)
	ctx := context.Background()

	tmpDir := t.TempDir()
	srcA := filepath.Join(tmpDir, "a.txt")
	srcB := filepath.Join(tmpDir, "b.txt")
	dataA := []byte("AAAA")
	dataB := []byte("BBBBBB")
	os.WriteFile(srcA, dataA, 0644)
	os.WriteFile(srcB, dataB, 0644)

	descs := []arcset.SegmentDesc{
		{FilePath: srcA, FileSize: int64(len(dataA)), FileOffset: 0, SegmentSize: int64(len(dataA)), FileID: 1},
		{FilePath: srcB, FileSize: int64(len(dataB)), FileOffset: 0, SegmentSize: int64(len(dataB)), FileID: 2},
	}

	outputDir := t.TempDir()
	err := shard.CreateShard(ctx, store, descs, 2, 1, outputDir, "DATA")
	if err != nil {
		t.Fatalf("CreateShard: %v", err)
	}

	shardFile := filepath.Join(outputDir, "shard_2_0001.pak")
	got, _ := os.ReadFile(shardFile)
	expected := string(dataA) + string(dataB)
	if string(got) != expected {
		t.Errorf("shard content: got %q, want %q", string(got), expected)
	}
}
