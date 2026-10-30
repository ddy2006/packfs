package dataset_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
)

func TestDatasetStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ds.db")
	sqlDB, err := db.OpenDatasetSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := dataset.NewDatasetStore(sqlDB)

	ds := &dataset.Dataset{
		UID:         "22222222-2222-4222-8222-222222222222",
		Name:        "store-ds",
		Status:      "building",
		CurrentPath: "/tmp/store-ds",
		Metadata:    map[string]any{"format": "bin"},
	}
	if err := store.Create(ctx, ds); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ds.ID == 0 {
		t.Fatal("create did not set id")
	}
	if got, err := store.FindByUID(ctx, ds.UID); err != nil || got.ID != ds.ID {
		t.Fatalf("find by uid: %+v err=%v", got, err)
	}
	if got, err := store.FindByName(ctx, "store-ds"); err != nil || got.ID != ds.ID {
		t.Fatalf("find by name: %+v err=%v", got, err)
	}

	files := []*dataset.File{
		{FilePath: "a.txt", FileSize: 3, Checksum: "aaa", Dataset: ds.ID},
		{FilePath: "b.txt", FileSize: 5, Checksum: "bbb", Dataset: ds.ID},
	}
	if err := store.BatchAddFileRecords(ctx, files); err != nil {
		t.Fatalf("batch add files: %v", err)
	}
	gotFiles, err := store.ListFiles(ctx, ds.ID)
	if err != nil || len(gotFiles) != 2 {
		t.Fatalf("list files: len=%d err=%v", len(gotFiles), err)
	}
	if gotFiles[0].FilePath != "a.txt" || gotFiles[1].FilePath != "b.txt" {
		t.Fatalf("file order: %+v", gotFiles)
	}

	ds.Metadata["shard_count"] = 7
	if err := store.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, ds.ID, "built"); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindByID(ctx, ds.ID)
	if err != nil || got.Status != "built" || got.Metadata["shard_count"] != float64(7) {
		t.Fatalf("updated dataset: %+v err=%v", got, err)
	}

	if err := store.Delete(ctx, ds.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindByID(ctx, ds.ID); err == nil {
		t.Fatal("dataset should be deleted")
	}
	if files, err := store.ListFiles(ctx, ds.ID); err != nil || len(files) != 0 {
		t.Fatalf("files after delete: %d err=%v", len(files), err)
	}
	sqlDB.Close()

	ro, err := db.OpenDatasetSQLite(path, true)
	if err != nil {
		t.Fatalf("reopen read-only: %v", err)
	}
	defer ro.Close()
	if _, err := dataset.NewDatasetStore(ro).FindByID(ctx, ds.ID); err == nil {
		t.Fatal("deleted dataset should not exist after reopen")
	}
}
