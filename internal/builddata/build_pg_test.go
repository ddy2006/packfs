package builddata_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/builddata"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/pgtest"
)

func TestBuildDataIdempotentAndRetry(t *testing.T) {
	pool := pgtest.Open(t)
	ctx := context.Background()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	shardDir := filepath.Join(tmp, "shards")
	arcDir := filepath.Join(tmp, "arc")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		name := filepath.Join(src, "f"+pad(i)+".dat")
		if err := os.WriteFile(name, []byte("0123456789abcdef"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	dsResult, err := datasetops.Create(ctx, datasetops.CreateOptions{
		RootDir:       src,
		Name:          "builddata-ds",
		DBDir:         filepath.Join(tmp, "db"),
		TargetRoot:    shardDir,
		Format:        "bin",
		ShardMaxBytes: 64,
	})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if dsResult.ShardCount != 10 {
		t.Fatalf("dataset shards=%d, want 10", dsResult.ShardCount)
	}

	arcResult, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
		Name:              "builddata-arc",
		TargetRoot:        arcDir,
		EC:                "4+2",
		Groups:            1,
		Layout:            "raid3",
		PartitionWidth:    2,
		SeqWidth:          3,
		PartitionCapacity: 1 << 30,
		ShardMaxBytes:     64,
	})
	if err != nil {
		t.Fatalf("create arcset: %v", err)
	}
	arcID := int64(arcResult.Arcset.ID)
	if _, err := arcsetops.Append(ctx, pool, arcsetops.AppendOptions{
		ArcsetID:  arcID,
		DatasetDB: dsResult.SQLitePath,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	n, err := builddata.BuildData(ctx, pool, builddata.Options{ArcsetID: arcID})
	if err != nil {
		t.Fatalf("build-data: %v", err)
	}
	if n != 10 {
		t.Fatalf("built=%d, want 10", n)
	}
	store := arcset.NewPGStore(pool)
	rows, err := store.ListShards(ctx, arcID)
	if err != nil || len(rows) != 10 {
		t.Fatalf("shard rows=%d err=%v", len(rows), err)
	}

	n, err = builddata.BuildData(ctx, pool, builddata.Options{ArcsetID: arcID})
	if err != nil {
		t.Fatalf("second build-data: %v", err)
	}
	if n != 0 {
		t.Fatalf("second build-data built=%d, want 0", n)
	}
	rows, _ = store.ListShards(ctx, arcID)
	if len(rows) != 10 {
		t.Fatalf("second build-data changed rows=%d", len(rows))
	}

	// Simulate a crash after the file moved but before the PG row committed:
	// delete one DATA row; the retry must restore it from the existing target.
	var lost *arcset.ShardRow
	for _, row := range rows {
		if row.Type == "DATA" {
			lost = row
			break
		}
	}
	if lost == nil {
		t.Fatal("no DATA row")
	}
	lostPath := lost.FilePath
	lostSum := *lost.SHA256
	if err := store.DeleteShard(ctx, lost.ID); err != nil {
		t.Fatal(err)
	}
	n, err = builddata.BuildData(ctx, pool, builddata.Options{ArcsetID: arcID})
	if err != nil {
		t.Fatalf("retry build-data: %v", err)
	}
	if n != 1 {
		t.Fatalf("retry built=%d, want 1", n)
	}
	restored, err := store.FindShardByPath(ctx, arcID, lostPath)
	if err != nil || restored == nil {
		t.Fatalf("restored row: %+v err=%v", restored, err)
	}
	sum, err := ec.SHA256File(filepath.Join(arcDir, lostPath))
	if err != nil || sum != lostSum {
		t.Fatalf("restored checksum=%s err=%v, want %s", sum, err, lostSum)
	}
	rows, _ = store.ListShards(ctx, arcID)
	if len(rows) != 10 {
		t.Fatalf("retry rows=%d, want 10", len(rows))
	}
}

func pad(n int) string {
	const digits = "0123456789"
	return string([]byte{digits[n/10%10], digits[n%10]})
}
