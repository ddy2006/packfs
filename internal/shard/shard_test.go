package shard_test

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ddy2006/packfs/internal/dataset"
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
		sha256 VARCHAR,
		metadata JSON,
		last_check DATETIME,
		arcset INTEGER,
		dataset INTEGER,
		CHECK (dataset IS NOT NULL OR arcset IS NOT NULL)
	);
	CREATE UNIQUE INDEX idx_t_shard__dataset_file_path
		ON t_shard (dataset, file_path) WHERE dataset IS NOT NULL;
	CREATE UNIQUE INDEX idx_t_shard__arcset_file_path
		ON t_shard (arcset, file_path) WHERE arcset IS NOT NULL;
	CREATE TABLE t_segment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		offset BIGINT,
		size BIGINT,
		csize BIGINT,
		shard INTEGER NOT NULL REFERENCES t_shard(id) ON DELETE CASCADE,
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

	descs := []dataset.SegmentDesc{
		{FilePath: srcFile, FileSize: int64(len(data)), FileOffset: 0, SegmentSize: int64(len(data)), FileID: 1},
	}

	outputDir := t.TempDir()
	err := shard.CreateShard(ctx, store, descs, 1, 1, 0, outputDir, "DATA")
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
	if shards[0].Dataset.Int64 != 1 {
		t.Errorf("Dataset: got %d, want 1", shards[0].Dataset.Int64)
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

	descs := []dataset.SegmentDesc{
		{FilePath: srcA, FileSize: int64(len(dataA)), FileOffset: 0, SegmentSize: int64(len(dataA)), FileID: 1},
		{FilePath: srcB, FileSize: int64(len(dataB)), FileOffset: 0, SegmentSize: int64(len(dataB)), FileID: 2},
	}

	outputDir := t.TempDir()
	err := shard.CreateShard(ctx, store, descs, 2, 2, 1, outputDir, "DATA")
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

func TestTarShardRoundtrip(t *testing.T) {
	db := setupDB(t)
	store := shard.NewSQLiteStore(db)
	ctx := context.Background()

	entries := []struct {
		name string
		data []byte
	}{
		{"a.txt", []byte("hello")},
		{"sub/b.txt", []byte("world")},
	}

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.name,
			Size: int64(len(e.data)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatalf("write tar entry: %v", err)
		}
	}
	tw.Close()

	outputDir := t.TempDir()
	shardPath := filepath.Join(outputDir, "0000.tar")
	if err := os.WriteFile(shardPath, tarBuf.Bytes(), 0644); err != nil {
		t.Fatalf("write shard: %v", err)
	}

	sh := &shard.Shard{
		FilePath: "0000.tar",
		FileSize: int64(len(tarBuf.Bytes())),
		Type:     "DATA",
		Checksum: "dummy",
		Arcset:   sql.NullInt64{Int64: 1, Valid: true},
		Dataset:  sql.NullInt64{Int64: 1, Valid: true},
	}
	if err := store.CreateShard(ctx, sh); err != nil {
		t.Fatalf("CreateShard: %v", err)
	}
	segs := []*shard.Segment{
		{Offset: 0, Size: int64(len(entries[0].data)), Shard: sh.ID, File: 1},
		{Offset: 512, Size: int64(len(entries[1].data)), Shard: sh.ID, File: 2},
	}
	if err := store.ReplaceSegments(ctx, sh.ID, segs); err != nil {
		t.Fatalf("ReplaceSegments: %v", err)
	}

	raw, err := os.ReadFile(shardPath)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	var gotEntries []struct {
		name string
		data []byte
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		content, _ := io.ReadAll(tr)
		gotEntries = append(gotEntries, struct {
			name string
			data []byte
		}{hdr.Name, content})
	}

	if len(gotEntries) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(gotEntries))
	}
	for i := range entries {
		if gotEntries[i].name != entries[i].name {
			t.Errorf("entry %d name: got %q, want %q", i, gotEntries[i].name, entries[i].name)
		}
		if string(gotEntries[i].data) != string(entries[i].data) {
			t.Errorf("entry %d data: got %q, want %q", i, string(gotEntries[i].data), string(entries[i].data))
		}
	}
}
