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
		relative_path VARCHAR NOT NULL,
		label VARCHAR,
		metadata JSON
	);
	CREATE TABLE t_file (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_path VARCHAR NOT NULL,
		file_size BIGINT,
		metadata JSON,
		ctime DATETIME,
		mtime DATETIME,
		checksum TEXT,
		dataset INTEGER REFERENCES t_dataset(id) ON DELETE SET NULL
	);
	CREATE TABLE t_arcset (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name VARCHAR NOT NULL,
		path_regex VARCHAR NOT NULL,
		label VARCHAR,
		create_time DATETIME,
		rait_type VARCHAR,
		metadata JSON,
		status VARCHAR,
		unit_bytes BIGINT,
		segment_bytes BIGINT,
		backend VARCHAR NOT NULL,
		sum_bytes BIGINT,
		net_bytes BIGINT,
		compress_algo TEXT,
		last_check DATETIME,
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
	res, err := db.Exec(`INSERT INTO t_dataset (name, relative_path, label, metadata)
		VALUES (?, ?, '', '{}')`, name, "/data/"+name)
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
		Name:         "arc1",
		PathRegex:    "/data/.*",
		Label:        "测试归档集",
		SegmentBytes: 1024,
		Backend:      "local",
		DatasetIDs:   []int{dsID},
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
	if a.SegmentBytes != 1024 {
		t.Errorf("SegmentBytes: got %d, want 1024", a.SegmentBytes)
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
		Name:         "arc2",
		PathRegex:    "/data/.*",
		Backend:      "local",
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

	store.Create(ctx, &arcset.Arcset{Name: "z", PathRegex: "/z", Backend: "local", Status: "ON"})
	store.Create(ctx, &arcset.Arcset{Name: "a", PathRegex: "/a", Backend: "local", Status: "OFF"})
	store.Create(ctx, &arcset.Arcset{Name: "m", PathRegex: "/m", Backend: "local", Status: "ON"})

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

	store.Create(ctx, &arcset.Arcset{Name: "upd", PathRegex: "/u", Backend: "local"})

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

	store.Create(ctx, &arcset.Arcset{Name: "x", PathRegex: "/x", Backend: "local"})
	store.Create(ctx, &arcset.Arcset{Name: "y", PathRegex: "/y", Backend: "local"})

	all, err := arcset.ListArcsets(ctx, store, arcset.Filter{})
	if err != nil {
		t.Fatalf("ListArcsets: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

func TestGenerateSegments(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	dsID := seedDataset(t, store.DB, "seg-ds")
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('a.txt', 3000, 'abc', ?)`, dsID)
	store.DB.Exec(`INSERT INTO t_file (file_path, file_size, checksum, dataset)
		VALUES ('b.txt', 1500, 'def', ?)`, dsID)

	params := arcset.CreateArcsetParams{
		Name:         "seg-arc",
		PathRegex:    "/data/.*",
		SegmentBytes: 1024,
		Backend:      "local",
		DatasetIDs:   []int{dsID},
	}
	if err := arcset.CreateArcset(ctx, store, params); err != nil {
		t.Fatalf("CreateArcset: %v", err)
	}

	a, _ := store.FindByName(ctx, "seg-arc")
	descs, err := arcset.GenerateSegments(ctx, store, a.ID)
	if err != nil {
		t.Fatalf("GenerateSegments: %v", err)
	}

	// a.txt 3000 bytes / 1024 = 3 segments, b.txt 1500 bytes / 1024 = 2 segments
	expectedCount := 5
	if len(descs) != expectedCount {
		t.Errorf("expected %d segments, got %d", expectedCount, len(descs))
	}

	// First segment: a.txt, offset 0, size 1024
	if descs[0].FilePath != "a.txt" || descs[0].FileOffset != 0 || descs[0].SegmentSize != 1024 {
		t.Errorf("first segment: %+v", descs[0])
	}
	// Fourth segment: b.txt, offset 0, size 1024
	if descs[3].FilePath != "b.txt" || descs[3].FileOffset != 0 || descs[3].SegmentSize != 1024 {
		t.Errorf("fourth segment: %+v", descs[3])
	}
	// Fifth segment: b.txt, offset 1024, size 476
	if descs[4].FilePath != "b.txt" || descs[4].FileOffset != 1024 || descs[4].SegmentSize != 476 {
		t.Errorf("fifth segment: %+v", descs[4])
	}
}
