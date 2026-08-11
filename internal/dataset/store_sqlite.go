package dataset

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"

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

func (s *SQLiteStore) FindByID(ctx context.Context, id int) (*Dataset, error) {
	query := `SELECT id, name, label, status, metadata, current_path, comment FROM t_dataset WHERE id = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, id))
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

func (s *SQLiteStore) Delete(ctx context.Context, id int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return errors.WrapE(err, "begin tx")
	}
	defer tx.Rollback()

	// Delete segments belonging to this dataset's shards
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM t_segment WHERE shard IN (SELECT id FROM t_shard WHERE dataset = ?)`, id); err != nil {
		return errors.WrapE(err, "delete segments")
	}

	// Delete shards belonging to this dataset
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM t_shard WHERE dataset = ?`, id); err != nil {
		return errors.WrapE(err, "delete shards")
	}

	// Delete arcset-dataset links
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM r_arcset_dataset WHERE dataset = ?`, id); err != nil {
		return errors.WrapE(err, "delete arcset links")
	}

	// Delete file records
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM t_file WHERE dataset = ?`, id); err != nil {
		return errors.WrapE(err, "delete files")
	}

	// Delete the dataset itself
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM t_dataset WHERE id = ?`, id); err != nil {
		return errors.WrapE(err, "delete dataset")
	}

	return errors.WrapE(tx.Commit(), "commit")
}

func (s *SQLiteStore) UpdateStatus(ctx context.Context, id int, status string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE t_dataset SET status = ? WHERE id = ?`, status, id)
	return errors.WrapE(err, "update dataset status", "id", id, "status", status)
}

func (s *SQLiteStore) BatchAddFileRecords(ctx context.Context, files []*File) error {
	if len(files) == 0 {
		return nil
	}
	// SQLite 缺省变量上限 999，每行 5 列 → 199 行安全，可通过环境变量覆盖
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

func batchSizeFromEnv(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
