package arcset_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ddy2006/packfs/internal/arcset"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	schema := `
	CREATE TABLE t_dataset (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name VARCHAR,
		label VARCHAR,
		metadata JSON NOT NULL,
		current_path VARCHAR,
		comment TEXT
	);
	CREATE TABLE t_file (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_path VARCHAR NOT NULL,
		file_size BIGINT,
		metadata JSON,
		checksum TEXT,
		dataset INTEGER NOT NULL REFERENCES t_dataset(id) ON DELETE CASCADE
	);
	CREATE TABLE t_arcset (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name VARCHAR NOT NULL,
		label VARCHAR,
		metadata JSON,
		status VARCHAR,
		last_check DATETIME,
		current_path VARCHAR,
		comment TEXT
	);
	CREATE TABLE r_arcset_dataset (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		arcset INTEGER NOT NULL REFERENCES t_arcset(id) ON DELETE CASCADE,
		dataset INTEGER NOT NULL REFERENCES t_dataset(id) ON DELETE CASCADE
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func newStore(t *testing.T) *arcset.SQLiteStore {
	t.Helper()
	return arcset.NewSQLiteStore(setupDB(t))
}

func seedDataset(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO t_dataset (name, label, metadata, current_path)
		VALUES (?, '', '{}', ?)`, name, "/data/"+name)
	if err != nil {
		t.Fatalf("seed dataset: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func TestCreateArcset(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "ds1")

	params := arcset.CreateArcsetParams{
		Name:       "arc1",
		Label:      "测试归档集",
		Metadata:   map[string]any{"shard_max_bytes": int64(1024), "format": "bin"},
		DatasetIDs: []int{dsID},
	}
	if err := arcset.CreateArcset(ctx, store, params); err != nil {
		t.Fatalf("CreateArcset: %v", err)
	}

	a, err := store.FindByName(ctx, "arc1")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if a.Name != "arc1" {
		t.Errorf("Name: got %q, want %q", a.Name, "arc1")
	}

	refs, err := store.ListDatasetRefs(ctx, a.ID)
	if err != nil {
		t.Fatalf("ListDatasetRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("expected 1 dataset ref, got %d", len(refs))
	}
	if refs[0].Name != "ds1" {
		t.Errorf("dataset name: got %q, want ds1", refs[0].Name)
	}
}

func TestFindArcsetByID(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	params := arcset.CreateArcsetParams{
		Name: "arc2",
	}
	if err := arcset.CreateArcset(ctx, store, params); err != nil {
		t.Fatalf("CreateArcset: %v", err)
	}

	a, err := store.FindByName(ctx, "arc2")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}

	byID, err := store.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.Name != "arc2" {
		t.Errorf("got %q, want arc2", byID.Name)
	}
}

func TestFindArcsets(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Create(ctx, &arcset.Arcset{Name: "z", Status: "ON"})
	store.Create(ctx, &arcset.Arcset{Name: "a", Status: "OFF"})
	store.Create(ctx, &arcset.Arcset{Name: "m", Status: "ON"})

	all, err := store.Find(ctx, arcset.Filter{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}
	if all[0].Name != "a" || all[1].Name != "m" || all[2].Name != "z" {
		t.Errorf("wrong order: %v", []string{all[0].Name, all[1].Name, all[2].Name})
	}

	on := "ON"
	onOnly, err := store.Find(ctx, arcset.Filter{Status: &on})
	if err != nil {
		t.Fatalf("Find with status: %v", err)
	}
	if len(onOnly) != 2 {
		t.Errorf("expected 2 ON arcsets, got %d", len(onOnly))
	}
}

func TestUpdateArcset(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Create(ctx, &arcset.Arcset{Name: "upd"})

	newLabel := "更新后"
	err := store.Update(ctx, "upd", arcset.Update{Label: &newLabel})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	a, _ := store.FindByName(ctx, "upd")
	if a.Label != newLabel {
		t.Errorf("Label: got %q, want %q", a.Label, newLabel)
	}
}

func TestListArcsets(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Create(ctx, &arcset.Arcset{Name: "x"})
	store.Create(ctx, &arcset.Arcset{Name: "y"})

	all, err := arcset.ListArcsets(ctx, store, arcset.Filter{})
	if err != nil {
		t.Fatalf("ListArcsets: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

func TestGenerateShardDefs(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "seg-ds")
	// a=500 + b=400 fits in one shard (900 < 1024)
	// c=300 would push first shard over, starts shard 1
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('a.txt', 500, 'abc', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('b.txt', 400, 'def', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('c.txt', 300, 'ghi', ?)`, dsID)

	params := arcset.CreateArcsetParams{
		Name:       "seg-arc",
		Metadata:   map[string]any{"shard_max_bytes": int64(1024)},
		DatasetIDs: []int{dsID},
	}
	if err := arcset.CreateArcset(ctx, store, params); err != nil {
		t.Fatalf("CreateArcset: %v", err)
	}

	a, _ := store.FindByName(ctx, "seg-arc")
	shards, err := arcset.GenerateShardDefs(ctx, store, a.ID)
	if err != nil {
		t.Fatalf("GenerateShardDefs: %v", err)
	}

	// a+b=900 < 1024 → shard 0; c=300 → shard 1
	if len(shards) != 2 {
		t.Fatalf("expected 2 shards, got %d", len(shards))
	}
	if shards[0].Seq != 0 || len(shards[0].Segments) != 2 {
		t.Errorf("shard 0: seq=%d, segments=%d", shards[0].Seq, len(shards[0].Segments))
	}
	if shards[1].Seq != 1 || len(shards[1].Segments) != 1 {
		t.Errorf("shard 1: seq=%d, segments=%d", shards[1].Seq, len(shards[1].Segments))
	}
	if shards[1].Segments[0].FilePath != "c.txt" {
		t.Errorf("shard 1 file: %s", shards[1].Segments[0].FilePath)
	}
}

func TestGenerateShardDefsNoLimit(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "seg-ds-no")
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('a.txt', 5000, 'abc', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('b.txt', 3000, 'def', ?)`, dsID)

	params := arcset.CreateArcsetParams{
		Name:       "seg-arc-no",
		DatasetIDs: []int{dsID},
	}
	if err := arcset.CreateArcset(ctx, store, params); err != nil {
		t.Fatalf("CreateArcset: %v", err)
	}

	a, _ := store.FindByName(ctx, "seg-arc-no")
	shards, err := arcset.GenerateShardDefs(ctx, store, a.ID)
	if err != nil {
		t.Fatalf("GenerateShardDefs: %v", err)
	}

	if len(shards) != 1 {
		t.Fatalf("expected 1 shard, got %d", len(shards))
	}
	if len(shards[0].Segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(shards[0].Segments))
	}
}

func TestGenerateShardDefsExactBoundary(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "boundary-ds")
	// Exactly equal to maxBytes: 4+6=10
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('a.txt', 4, 'abc', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('b.txt', 6, 'def', ?)`, dsID)

	params := arcset.CreateArcsetParams{
		Name:       "boundary-arc",
		Metadata:   map[string]any{"shard_max_bytes": int64(10)},
		DatasetIDs: []int{dsID},
	}
	arcset.CreateArcset(ctx, store, params)
	a, _ := store.FindByName(ctx, "boundary-arc")
	shards, err := arcset.GenerateShardDefs(ctx, store, a.ID)
	if err != nil {
		t.Fatalf("GenerateShardDefs: %v", err)
	}

	// a=4 + b=6 = 10 == maxBytes → 同一个 shard
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard (exact boundary), got %d", len(shards))
	}
	if len(shards[0].Segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(shards[0].Segments))
	}
}

func TestGenerateShardDefsOverBoundary(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "over-ds")
	// 4+7=11 > 10, should split
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('a.txt', 4, 'abc', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('b.txt', 7, 'def', ?)`, dsID)

	params := arcset.CreateArcsetParams{
		Name:       "over-arc",
		Metadata:   map[string]any{"shard_max_bytes": int64(10)},
		DatasetIDs: []int{dsID},
	}
	arcset.CreateArcset(ctx, store, params)
	a, _ := store.FindByName(ctx, "over-arc")
	shards, err := arcset.GenerateShardDefs(ctx, store, a.ID)
	if err != nil {
		t.Fatalf("GenerateShardDefs: %v", err)
	}

	// a=4 → shard 0, b=7 → shard 1 (4+7=11 > 10)
	if len(shards) != 2 {
		t.Fatalf("expected 2 shards, got %d", len(shards))
	}
}
