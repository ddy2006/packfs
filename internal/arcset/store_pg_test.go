package arcset_test

import (
	"context"
	"testing"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/pgtest"
)

func TestPGStoreLifecycle(t *testing.T) {
	pool := pgtest.Open(t)
	ctx := context.Background()
	store := arcset.NewPGStore(pool)

	a, err := store.Create(ctx, arcset.CreateParams{
		Name:        "pg-store-arc",
		CurrentPath: "/tmp/pg-store-arc",
		Metadata:    map[string]any{"ec": "4+2", "group_count": 1},
	})
	if err != nil {
		t.Fatalf("create arcset: %v", err)
	}
	if a.Status != "defined" || a.BuildPhase != "none" || a.Health != "ok" {
		t.Fatalf("unexpected initial state: %+v", a)
	}
	if _, err := store.FindByName(ctx, "pg-store-arc"); err != nil {
		t.Fatalf("find by name: %v", err)
	}

	uid := "11111111-1111-4111-8111-111111111111"
	rel, err := store.AppendDataset(ctx, arcset.AppendDatasetParams{
		ArcsetID:        int64(a.ID),
		UID:             uid,
		Name:            "ds1",
		SQLitePath:      "/tmp/ds1.db",
		DatasetKey:      "ds11111111",
		DatasetMetadata: map[string]any{"format": "bin"},
	})
	if err != nil {
		t.Fatalf("append dataset: %v", err)
	}
	if rel.AppendOrder != 0 || rel.DatasetKey != "ds11111111" {
		t.Fatalf("unexpected relation: %+v", rel)
	}
	a, err = store.FindByID(ctx, int64(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "building" || a.BuildPhase != "data" {
		t.Fatalf("append should set building/data, got %s/%s", a.Status, a.BuildPhase)
	}

	// Idempotent append of the same dataset.
	rel2, err := store.AppendDataset(ctx, arcset.AppendDatasetParams{
		ArcsetID:   int64(a.ID),
		UID:        uid,
		Name:       "ds1",
		SQLitePath: "/tmp/ds1.db",
		DatasetKey: "ds11111111",
	})
	if err != nil {
		t.Fatalf("idempotent append: %v", err)
	}
	if rel2.ID != rel.ID || rel2.AppendOrder != rel.AppendOrder {
		t.Fatalf("idempotent append changed relation: %+v vs %+v", rel2, rel)
	}

	// The same dataset cannot join a second arcset.
	a2, err := store.Create(ctx, arcset.CreateParams{Name: "pg-store-arc2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendDataset(ctx, arcset.AppendDatasetParams{
		ArcsetID:   int64(a2.ID),
		UID:        uid,
		Name:       "ds1",
		SQLitePath: "/tmp/ds1.db",
		DatasetKey: "ds11111111",
	}); err == nil {
		t.Fatal("expected cross-arcset append to fail")
	}

	relations, err := store.ListRelations(ctx, int64(a.ID))
	if err != nil || len(relations) != 1 || relations[0].UID != uid {
		t.Fatalf("list relations: %+v err=%v", relations, err)
	}

	datasetRef := rel.DatasetRef
	sha := "abc"
	row := &arcset.ShardRow{
		ArcsetID:   int64(a.ID),
		DatasetRef: &datasetRef,
		DatasetKey: ptr("ds11111111"),
		Seq:        ptr(0),
		StripeSeq:  ptr(0),
		GroupNo:    ptr(0),
		Pos:        ptr(0),
		Partition:  ptr(0),
		FilePath:   "00/000D-ds11111111-0000.bin",
		FileSize:   123,
		Type:       "DATA",
		Metadata:   map[string]any{"data_bytes": 123},
		SHA256:     &sha,
	}
	if err := store.InsertShard(ctx, row); err != nil {
		t.Fatalf("insert DATA shard: %v", err)
	}
	ec := &arcset.ShardRow{
		ArcsetID:  int64(a.ID),
		StripeSeq: ptr(0),
		GroupNo:   ptr(0),
		Pos:       ptr(4),
		Partition: ptr(4),
		FilePath:  "04/000E.bin",
		FileSize:  123,
		Type:      "EC",
		Metadata:  map[string]any{"padded_size": 123},
	}
	if err := store.InsertShard(ctx, ec); err != nil {
		t.Fatalf("insert EC shard: %v", err)
	}
	rows, err := store.ListShards(ctx, int64(a.ID))
	if err != nil || len(rows) != 2 {
		t.Fatalf("list shards: len=%d err=%v", len(rows), err)
	}
	found, err := store.FindShardByPath(ctx, int64(a.ID), row.FilePath)
	if err != nil || found == nil || found.ID != row.ID {
		t.Fatalf("find shard by path: %+v err=%v", found, err)
	}
	if err := store.UpdateShardMetadata(ctx, row.ID, map[string]any{"padded_size": 123}); err != nil {
		t.Fatalf("update shard metadata: %v", err)
	}
	if err := store.SetParityDegraded(ctx, int64(a.ID), true); err != nil {
		t.Fatalf("set parity degraded: %v", err)
	}

	mount, err := store.AddMount(ctx, "/mnt/packfs-test", "dataset", &uid, nil)
	if err != nil {
		t.Fatalf("add mount: %v", err)
	}
	mounts, err := store.ListMounts(ctx)
	if err != nil || len(mounts) != 1 || mounts[0].ID != mount.ID {
		t.Fatalf("list mounts: %+v err=%v", mounts, err)
	}
	if err := store.DeleteMount(ctx, "/mnt/packfs-test"); err != nil {
		t.Fatalf("delete mount: %v", err)
	}

	if err := store.SetBuildPhase(ctx, int64(a.ID), "ec"); err != nil {
		t.Fatalf("set build phase: %v", err)
	}
	if err := store.CompleteBuild(ctx, int64(a.ID)); err != nil {
		t.Fatalf("complete build: %v", err)
	}
	a, err = store.FindByID(ctx, int64(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "completed" || a.BuildPhase != "none" || a.Health != "ok" || a.ParityDegraded {
		t.Fatalf("unexpected completed state: %+v", a)
	}
}

func ptr[T any](v T) *T { return &v }
