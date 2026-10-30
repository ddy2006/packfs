package datasetops_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/datasetops"
)

func TestCreateBuildScanMarkBuilt(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dbDir := filepath.Join(tmp, "db")
	shardDir := filepath.Join(tmp, "shards")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.dat", "b.dat", "c.dat"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("0123456789"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := datasetops.Create(context.Background(), datasetops.CreateOptions{
		RootDir:       src,
		Name:          "ops-ds",
		DBDir:         dbDir,
		TargetRoot:    shardDir,
		Format:        "bin",
		ShardMaxBytes: 10,
		GenOnly:       true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.Dataset.Status != "building" || result.ShardCount != 0 {
		t.Fatalf("gen-only result: status=%s shards=%d", result.Dataset.Status, result.ShardCount)
	}

	infos, err := datasetops.Scan(context.Background(), dbDir)
	if err != nil || len(infos) != 1 {
		t.Fatalf("scan: len=%d err=%v", len(infos), err)
	}
	if infos[0].Dataset.UID != result.Dataset.UID {
		t.Fatalf("scan uid=%s", infos[0].Dataset.UID)
	}
	info, err := datasetops.LoadInfoByUID(context.Background(), dbDir, result.Dataset.UID)
	if err != nil || info.Dataset.Name != "ops-ds" {
		t.Fatalf("load: %+v err=%v", info, err)
	}

	ds, count, err := datasetops.BuildShards(context.Background(), result.SQLitePath)
	if err != nil {
		t.Fatalf("build shards: %v", err)
	}
	if count != 3 || ds.Status != "built" {
		t.Fatalf("build result: count=%d status=%s", count, ds.Status)
	}
	shards, err := os.ReadDir(shardDir)
	if err != nil || len(shards) != 3 {
		t.Fatalf("shard files: %d err=%v", len(shards), err)
	}

	ds, count, err = datasetops.MarkBuilt(context.Background(), result.SQLitePath)
	if err != nil || count != 3 || ds.Status != "built" {
		t.Fatalf("mark built: count=%d status=%s err=%v", count, ds.Status, err)
	}

	// Corrupt a shard and verify MarkBuilt rejects it.
	shardFile := filepath.Join(shardDir, shards[0].Name())
	if err := os.WriteFile(shardFile, []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := datasetops.MarkBuilt(context.Background(), result.SQLitePath); err == nil {
		t.Fatal("expected checksum failure")
	}
}

func TestValidateUID(t *testing.T) {
	if err := dataset.ValidateUID("11111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"", "../x", "a/b", ".", ".."} {
		if err := dataset.ValidateUID(uid); err == nil {
			t.Fatalf("ValidateUID(%q) expected error", uid)
		}
	}
}
