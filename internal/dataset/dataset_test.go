package dataset_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ddy2006/packfs/internal/dataset"
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
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func newStore(t *testing.T) *dataset.SQLiteStore {
	t.Helper()
	return dataset.NewSQLiteStore(setupDB(t))
}

func TestCreateAndFindByName(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	ds := &dataset.Dataset{
		Name:         "test-ds",
		RelativePath: "/data/test",
		Label:        "测试数据集",
	}
	if err := store.Create(ctx, ds); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ds.ID == 0 {
		t.Error("expected ID to be set after create")
	}

	got, err := store.FindByName(ctx, "test-ds")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if got.Name != ds.Name {
		t.Errorf("Name: got %q, want %q", got.Name, ds.Name)
	}
	if got.RelativePath != ds.RelativePath {
		t.Errorf("RelativePath: got %q, want %q", got.RelativePath, ds.RelativePath)
	}
	if got.Label != ds.Label {
		t.Errorf("Label: got %q, want %q", got.Label, ds.Label)
	}
}

func TestFindByNameNotFound(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	_, err := store.FindByName(ctx, "no-such-ds")
	if err == nil {
		t.Fatal("expected error for missing dataset")
	}
}

func TestFind(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"b", "a", "c"} {
		if err := store.Create(ctx, &dataset.Dataset{
			Name:         name,
			RelativePath: "/data/" + name,
		}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	all, err := store.Find(ctx, dataset.Filter{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 datasets, got %d", len(all))
	}
	if all[0].Name != "a" || all[1].Name != "b" || all[2].Name != "c" {
		t.Errorf("wrong order: %v", []string{all[0].Name, all[1].Name, all[2].Name})
	}

	limit := 1
	limited, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		t.Fatalf("Find with limit: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("expected 1 dataset with limit, got %d", len(limited))
	}
}

func TestAddFileRecordAndListFiles(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	ds := &dataset.Dataset{
		Name:         "ds1",
		RelativePath: "/data/ds1",
	}
	if err := store.Create(ctx, ds); err != nil {
		t.Fatalf("Create: %v", err)
	}

	f1 := &dataset.File{
		FilePath: "file1.txt",
		FileSize: 100,
		Checksum: "abc123",
		Dataset:  ds.ID,
	}
	if err := store.AddFileRecord(ctx, f1); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
	}

	f2 := &dataset.File{
		FilePath: "subdir/file2.txt",
		FileSize: 200,
		Checksum: "def456",
		Dataset:  ds.ID,
	}
	if err := store.AddFileRecord(ctx, f2); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
	}

	files, err := store.ListFiles(ctx, ds.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file in current dir, got %d", len(files))
	}
	if files[0].FilePath != "file1.txt" {
		t.Errorf("got file %q, want file1.txt", files[0].FilePath)
	}
}

func TestCreateFromDir(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	fileContents := map[string]string{
		"a.txt": "hello",
		"b.txt": "world",
	}
	for name, content := range fileContents {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
	}

	if err := dataset.CreateFromDir(ctx, store, tmpDir, "from-dir-ds"); err != nil {
		t.Fatalf("CreateFromDir: %v", err)
	}

	ds, err := store.FindByName(ctx, "from-dir-ds")
	if err != nil {
		t.Fatalf("FindByName after CreateFromDir: %v", err)
	}
	if ds.RelativePath != tmpDir {
		t.Errorf("RelativePath: got %q, want %q", ds.RelativePath, tmpDir)
	}

	gotFiles, err := store.ListFiles(ctx, ds.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(gotFiles) != 2 {
		t.Errorf("expected 2 files, got %d", len(gotFiles))
	}
}

func TestListDatasets(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	store.Create(ctx, &dataset.Dataset{Name: "x", RelativePath: "/x"})
	store.Create(ctx, &dataset.Dataset{Name: "y", RelativePath: "/y"})

	all, err := dataset.ListDatasets(ctx, store, dataset.Filter{})
	if err != nil {
		t.Fatalf("ListDatasets: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 datasets, got %d", len(all))
	}
}
