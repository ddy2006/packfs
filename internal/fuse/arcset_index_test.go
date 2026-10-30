package fuse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/builddata"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/ddy2006/packfs/internal/pgtest"
)

func TestBuildArcsetIndex(t *testing.T) {
	pool := pgtest.Open(t)
	ctx := context.Background()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	shardDir := filepath.Join(tmp, "shards")
	arcDir := filepath.Join(tmp, "arc")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("bravo"), 0644); err != nil {
		t.Fatal(err)
	}
	dsResult, err := datasetops.Create(ctx, datasetops.CreateOptions{
		RootDir:       src,
		Name:          "fuse-ds",
		DBDir:         filepath.Join(tmp, "db"),
		TargetRoot:    shardDir,
		Format:        "bin",
		ShardMaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	arcResult, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
		Name:              "fuse-arc",
		TargetRoot:        arcDir,
		EC:                "4+2",
		Groups:            1,
		PartitionCapacity: 1 << 30,
		ShardMaxBytes:     1 << 20,
	})
	if err != nil {
		t.Fatalf("create arcset: %v", err)
	}
	arcID := int64(arcResult.Arcset.ID)
	appendResult, err := arcsetops.Append(ctx, pool, arcsetops.AppendOptions{
		ArcsetID:  arcID,
		DatasetDB: dsResult.SQLitePath,
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := builddata.BuildData(ctx, pool, builddata.Options{ArcsetID: arcID}); err != nil {
		t.Fatalf("build-data: %v", err)
	}

	idx, err := BuildArcsetIndex(ctx, pool, arcID)
	if err != nil {
		t.Fatalf("build arcset index: %v", err)
	}
	if len(idx.Files) != 2 {
		t.Fatalf("index files=%d, want 2", len(idx.Files))
	}
	prefix := appendResult.DatasetKey + "/"
	for path := range idx.Files {
		if !strings.HasPrefix(path, prefix) {
			t.Fatalf("index path %q does not start with %q", path, prefix)
		}
	}
	tree := idx.DirTree()
	if tree.Children[appendResult.DatasetKey] == nil {
		t.Fatalf("missing dataset_key directory %q", appendResult.DatasetKey)
	}
}
