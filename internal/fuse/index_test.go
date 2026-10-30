package fuse

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/ddy2006/packfs/internal/db"
)

func buildTestIndex(t *testing.T) *Index {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	shardDir := filepath.Join(tmp, "shards")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello packfs"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("fuse test"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := datasetops.Create(context.Background(), datasetops.CreateOptions{
		RootDir:       src,
		DBDir:         filepath.Join(tmp, "db"),
		TargetRoot:    shardDir,
		Format:        "bin",
		ShardMaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	dsDB, err := db.OpenDatasetSQLite(result.SQLitePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer dsDB.Close()
	idx, err := BuildIndex(context.Background(), dsDB, result.Dataset.ID, shardDir, "")
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return idx
}

func TestBuildIndexAndDirTree(t *testing.T) {
	idx := buildTestIndex(t)
	if len(idx.Files) != 2 {
		t.Fatalf("index files=%d, want 2", len(idx.Files))
	}
	if got := totalSize(idx.Files["a.txt"]); got != int64(len("hello packfs")) {
		t.Fatalf("a.txt size=%d", got)
	}
	tree := idx.DirTree()
	if tree.Children["a.txt"] == nil || tree.Children["a.txt"].IsDir() {
		t.Fatalf("a.txt missing from dir tree")
	}
	sub := tree.Children["sub"]
	if sub == nil || !sub.IsDir() || sub.Children["b.txt"] == nil {
		t.Fatalf("sub/b.txt missing from dir tree")
	}
}

func totalSize(segs []SegmentLoc) int64 {
	var n int64
	for _, s := range segs {
		n += s.Size
	}
	return n
}
