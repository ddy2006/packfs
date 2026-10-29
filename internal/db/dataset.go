package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed dataset_schema.sql
var datasetSchemaSQL string

// OpenDatasetSQLite opens one dataset SQLite file.
//
// readOnly uses a read-only DSN; it must not create or migrate the file.
// A writable open creates the parent directory and applies the dataset schema
// when the file does not exist yet.
func OpenDatasetSQLite(path string, readOnly bool) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("dataset sqlite path is empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve dataset sqlite path: %w", err)
	}

	if readOnly {
		if _, err := os.Stat(absPath); err != nil {
			return nil, fmt.Errorf("dataset sqlite not found: %s: %w", absPath, err)
		}
		db, err := sql.Open("sqlite3", "file:"+absPath+"?mode=ro&_foreign_keys=on")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(4)
		return db, nil
	}

	needInit := false
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		needInit = true
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, fmt.Errorf("create dataset sqlite directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", "file:"+absPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	// A dataset SQLite has a single writer (the control plane); keep writes
	// serialized and avoid SQLITE_BUSY between concurrent connections.
	db.SetMaxOpenConns(1)

	if needInit {
		if _, err := db.Exec(datasetSchemaSQL); err != nil {
			db.Close()
			return nil, fmt.Errorf("init dataset sqlite schema: %w", err)
		}
	}
	return db, nil
}

// DatasetSchemaSQL returns the embedded dataset schema (used by tests).
func DatasetSchemaSQL() string { return datasetSchemaSQL }
