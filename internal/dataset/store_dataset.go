package dataset

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// DatasetStore implements Store against one per-dataset SQLite file.
type DatasetStore struct {
	DB *sql.DB
}

func NewDatasetStore(db *sql.DB) *DatasetStore { return &DatasetStore{DB: db} }

func (s *DatasetStore) Create(ctx context.Context, ds *Dataset) error {
	if ds.UID == "" {
		uid, err := NewUID()
		if err != nil {
			return err
		}
		ds.UID = uid
	}
	if ds.Status == "" {
		ds.Status = "building"
	}
	metadataJSON, _ := json.Marshal(ds.Metadata)
	result, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_dataset (uid, name, status, metadata, current_path, comment)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ds.UID, ds.Name, ds.Status, string(metadataJSON), ds.CurrentPath, nullText(ds.Comment))
	if err != nil {
		return errors.WrapE(err, "create dataset", "name", ds.Name)
	}
	id, _ := result.LastInsertId()
	ds.ID = int(id)
	return nil
}

func (s *DatasetStore) FindByID(ctx context.Context, id int) (*Dataset, error) {
	query := `SELECT id, uid, name, status, metadata, current_path, comment
	          FROM t_dataset WHERE id = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, id))
}

func (s *DatasetStore) FindByUID(ctx context.Context, uid string) (*Dataset, error) {
	query := `SELECT id, uid, name, status, metadata, current_path, comment
	          FROM t_dataset WHERE uid = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, uid))
}

func (s *DatasetStore) FindByName(ctx context.Context, name string) (*Dataset, error) {
	query := `SELECT id, uid, name, status, metadata, current_path, comment
	          FROM t_dataset WHERE name = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, name))
}

func (s *DatasetStore) Find(ctx context.Context, filter Filter) ([]*Dataset, error) {
	query := `SELECT id, uid, name, status, metadata, current_path, comment
	          FROM t_dataset WHERE 1=1`
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

func (s *DatasetStore) UpdateMetadata(ctx context.Context, id int, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	_, err := s.DB.ExecContext(ctx,
		`UPDATE t_dataset SET metadata = ? WHERE id = ?`, string(metadataJSON), id)
	return errors.WrapE(err, "update dataset metadata", "id", id)
}

func (s *DatasetStore) UpdateStatus(ctx context.Context, id int, status string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE t_dataset SET status = ? WHERE id = ?`, status, id)
	return errors.WrapE(err, "update dataset status", "id", id, "status", status)
}

func (s *DatasetStore) Delete(ctx context.Context, id int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM t_dataset WHERE id = ?`, id)
	return errors.WrapE(err, "delete dataset", "id", id)
}

func (s *DatasetStore) AddFileRecord(ctx context.Context, f *File) error {
	metadataJSON, _ := json.Marshal(f.Metadata)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_file (file_path, file_size, metadata, sha256, dataset)
		 VALUES (?, ?, ?, ?, ?)`,
		f.FilePath, f.FileSize, string(metadataJSON), f.Checksum, f.Dataset)
	return errors.WrapE(err, "add file record", "file_path", f.FilePath)
}

func (s *DatasetStore) BatchAddFileRecords(ctx context.Context, files []*File) error {
	if len(files) == 0 {
		return nil
	}
	batch := batchSizeFromEnv("PACKFS_BATCH_INSERT", 199)
	for i := 0; i < len(files); i += batch {
		end := i + batch
		if end > len(files) {
			end = len(files)
		}
		chunk := files[i:end]
		query := "INSERT INTO t_file (file_path, file_size, metadata, sha256, dataset) VALUES "
		var args []any
		for j, f := range chunk {
			if j > 0 {
				query += ", "
			}
			query += "(?, ?, ?, ?, ?)"
			metadataJSON, _ := json.Marshal(f.Metadata)
			args = append(args, f.FilePath, f.FileSize, string(metadataJSON), f.Checksum, f.Dataset)
		}
		if _, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			return errors.WrapE(err, "batch add file records", "start", i, "count", len(chunk))
		}
	}
	return nil
}

func (s *DatasetStore) ListFiles(ctx context.Context, datasetID int) ([]*File, error) {
	query := `SELECT file_path, file_size, metadata, sha256, dataset
	          FROM t_file WHERE dataset = ? ORDER BY file_path`
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

func (s *DatasetStore) scanOne(row *sql.Row) (*Dataset, error) {
	ds := &Dataset{}
	var metadata []byte
	var currentPath, comment sql.NullString
	err := row.Scan(&ds.ID, &ds.UID, &ds.Name, &ds.Status, &metadata, &currentPath, &comment)
	if err == sql.ErrNoRows {
		return nil, errors.E("dataset not found")
	}
	if err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.CurrentPath = currentPath.String
	ds.Comment = comment.String
	ds.Metadata = make(map[string]any)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &ds.Metadata); err != nil {
			logrus.Errorf("unmarshal dataset metadata failed: %v", err)
		}
	}
	return ds, nil
}

func (s *DatasetStore) scanRow(rows *sql.Rows) (*Dataset, error) {
	ds := &Dataset{}
	var metadata []byte
	var currentPath, comment sql.NullString
	if err := rows.Scan(&ds.ID, &ds.UID, &ds.Name, &ds.Status, &metadata, &currentPath, &comment); err != nil {
		return nil, errors.WrapE(err, "scan dataset row")
	}
	ds.CurrentPath = currentPath.String
	ds.Comment = comment.String
	ds.Metadata = make(map[string]any)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &ds.Metadata); err != nil {
			logrus.Errorf("unmarshal dataset metadata failed: %v", err)
		}
	}
	return ds, nil
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
