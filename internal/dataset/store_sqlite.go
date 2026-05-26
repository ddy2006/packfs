package dataset

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

func (s *SQLiteStore) Create(ctx context.Context, ds *Dataset) error {
	metadataJSON, _ := json.Marshal(ds.Metadata)
	result, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_dataset (name, relative_path, label, metadata)
		 VALUES (?, ?, ?, ?)`,
		ds.Name, ds.RelativePath, ds.Label, string(metadataJSON))
	if err != nil {
		return errors.WrapE(err, "create dataset", "name", ds.Name)
	}
	id, _ := result.LastInsertId()
	ds.ID = int(id)
	return nil
}

func (s *SQLiteStore) FindByName(ctx context.Context, name string) (*Dataset, error) {
	query := `SELECT id, name, relative_path, label, metadata FROM t_dataset WHERE name = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, name))
}

func (s *SQLiteStore) Find(ctx context.Context, filter Filter) ([]*Dataset, error) {
	query := `SELECT id, name, relative_path, label, metadata FROM t_dataset WHERE 1=1`
	var args []any

	if filter.Limit != nil {
		query += " ORDER BY name LIMIT ?"
		args = append(args, *filter.Limit)
	} else {
		query += " ORDER BY name"
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.WrapE(err, "find datasets")
	}
	defer rows.Close()

	var datasets []*Dataset
	for rows.Next() {
		ds, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		datasets = append(datasets, ds)
	}
	return datasets, rows.Err()
}

func (s *SQLiteStore) AddFileRecord(ctx context.Context, f *File) error {
	metadataJSON, _ := json.Marshal(f.Metadata)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_file (file_path, file_size, metadata, ctime, mtime, checksum, dataset)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.FilePath, f.FileSize, string(metadataJSON), f.Ctime, f.Mtime, f.Checksum, f.Dataset)
	return errors.WrapE(err, "add file record", "file_path", f.FilePath)
}

func (s *SQLiteStore) ListFiles(ctx context.Context, datasetID int) ([]*File, error) {
	query := `SELECT file_path, file_size, metadata, ctime, mtime, checksum, dataset
	           FROM t_file WHERE dataset = ? AND file_path NOT LIKE '%/%' ORDER BY file_path`

	rows, err := s.DB.QueryContext(ctx, query, datasetID)
	if err != nil {
		return nil, errors.WrapE(err, "list files", "dataset_id", datasetID)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		f := &File{}
		var metadataBytes []byte
		if err := rows.Scan(&f.FilePath, &f.FileSize, &metadataBytes, &f.Ctime, &f.Mtime, &f.Checksum, &f.Dataset); err != nil {
			return nil, errors.WrapE(err, "scan file row")
		}
		f.Metadata = make(map[string]interface{})
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &f.Metadata); err != nil {
				logrus.Errorf("unmarshal file metadata failed: %v", err)
			}
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *SQLiteStore) scanOne(row *sql.Row) (*Dataset, error) {
	ds := &Dataset{}
	var metadata []byte
	err := row.Scan(&ds.ID, &ds.Name, &ds.RelativePath, &ds.Label, &metadata)
	if err == sql.ErrNoRows {
		return nil, errors.E("dataset not found")
	}
	if err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.Metadata = make(map[string]interface{})
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &ds.Metadata); err != nil {
			logrus.Errorf("unmarshal dataset metadata failed: %v", err)
		}
	}
	return ds, nil
}

func (s *SQLiteStore) scanRow(rows *sql.Rows) (*Dataset, error) {
	ds := &Dataset{}
	var metadata []byte
	if err := rows.Scan(&ds.ID, &ds.Name, &ds.RelativePath, &ds.Label, &metadata); err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.Metadata = make(map[string]interface{})
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &ds.Metadata); err != nil {
			logrus.Errorf("unmarshal dataset metadata failed: %v", err)
		}
	}
	return ds, nil
}
