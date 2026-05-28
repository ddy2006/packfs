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
	var result sql.Result
	var err error
	if ds.Comment != "" {
		result, err = s.DB.ExecContext(ctx,
			`INSERT INTO t_dataset (name, label, status, metadata, current_path, comment)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			ds.Name, ds.Label, ds.Status, string(metadataJSON), ds.CurrentPath, ds.Comment)
	} else {
		result, err = s.DB.ExecContext(ctx,
			`INSERT INTO t_dataset (name, label, status, metadata, current_path)
			 VALUES (?, ?, ?, ?, ?)`,
			ds.Name, ds.Label, ds.Status, string(metadataJSON), ds.CurrentPath)
	}
	if err != nil {
		return errors.WrapE(err, "create dataset", "name", ds.Name)
	}
	id, _ := result.LastInsertId()
	ds.ID = int(id)
	return nil
}

func (s *SQLiteStore) UpdateMetadata(ctx context.Context, id int, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	_, err := s.DB.ExecContext(ctx,
		`UPDATE t_dataset SET metadata = ? WHERE id = ?`,
		string(metadataJSON), id)
	return errors.WrapE(err, "update dataset metadata", "id", id)
}

func (s *SQLiteStore) FindByName(ctx context.Context, name string) (*Dataset, error) {
	query := `SELECT id, name, label, status, metadata, current_path, comment FROM t_dataset WHERE name = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, name))
}

func (s *SQLiteStore) Find(ctx context.Context, filter Filter) ([]*Dataset, error) {
	query := `SELECT id, name, label, status, metadata, current_path, comment FROM t_dataset WHERE 1=1`
	var args []any

	if filter.ID != nil {
		query += " AND id = ?"
		args = append(args, *filter.ID)
	}
	if filter.Name != nil {
		query += " AND name LIKE ?"
		args = append(args, "%"+*filter.Name+"%")
	}
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
		`INSERT INTO t_file (file_path, file_size, metadata, sha256, dataset)
		 VALUES (?, ?, ?, ?, ?)`,
		f.FilePath, f.FileSize, string(metadataJSON), f.Checksum, f.Dataset)
	return errors.WrapE(err, "add file record", "file_path", f.FilePath)
}

func (s *SQLiteStore) ListFiles(ctx context.Context, datasetID int) ([]*File, error) {
	query := `SELECT file_path, file_size, metadata, sha256, dataset
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
		if err := rows.Scan(&f.FilePath, &f.FileSize, &metadataBytes, &f.Checksum, &f.Dataset); err != nil {
			return nil, errors.WrapE(err, "scan file row")
		}
		f.Metadata = make(map[string]any)
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
	var comment sql.NullString
	err := row.Scan(&ds.ID, &ds.Name, &ds.Label, &ds.Status, &metadata, &ds.CurrentPath, &comment)
	if err == sql.ErrNoRows {
		return nil, errors.E("dataset not found")
	}
	if err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.Comment = comment.String
	ds.Metadata = make(map[string]any)
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
	var comment sql.NullString
	if err := rows.Scan(&ds.ID, &ds.Name, &ds.Label, &ds.Status, &metadata, &ds.CurrentPath, &comment); err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.Comment = comment.String
	ds.Metadata = make(map[string]any)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &ds.Metadata); err != nil {
			logrus.Errorf("unmarshal dataset metadata failed: %v", err)
		}
	}
	return ds, nil
}
