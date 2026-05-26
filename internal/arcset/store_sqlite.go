package arcset

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

func (s *SQLiteStore) Create(ctx context.Context, a *Arcset) error {
	metadataJSON, _ := json.Marshal(a.Metadata)
	result, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_arcset (name, path_regex, label, create_time, rait_type, metadata, status,
		 unit_bytes, segment_bytes, backend, sum_bytes, net_bytes, compress_algo, last_check, comment)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.PathRegex, nullStr(a.Label), a.CreateTime, nullStr(a.RaitType), string(metadataJSON), nullStr(a.Status),
		a.UnitBytes, a.SegmentBytes, a.Backend, nullInt(a.SumBytes), nullInt(a.NetBytes), nullStr(a.CompressAlgo), nullTime(a.LastCheck), nullStr(a.Comment))
	if err != nil {
		return errors.WrapE(err, "create arcset", "name", a.Name)
	}
	id, _ := result.LastInsertId()
	a.ID = int(id)
	return nil
}

func nullStr(s string) any { if s == "" { return nil }; return s }
func nullInt(n int64) any { if n == 0 { return nil }; return n }
func nullTime(t time.Time) any { if t.IsZero() { return nil }; return t }

func (s *SQLiteStore) FindByID(ctx context.Context, id int) (*Arcset, error) {
	query := `SELECT id, name, path_regex, label, create_time, rait_type, metadata, status,
	           unit_bytes, segment_bytes, backend, sum_bytes, net_bytes, compress_algo, last_check, comment
	           FROM t_arcset WHERE id = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, id))
}

func (s *SQLiteStore) FindByName(ctx context.Context, name string) (*Arcset, error) {
	query := `SELECT id, name, path_regex, label, create_time, rait_type, metadata, status,
	           unit_bytes, segment_bytes, backend, sum_bytes, net_bytes, compress_algo, last_check, comment
	           FROM t_arcset WHERE name = ?`
	return s.scanOne(s.DB.QueryRowContext(ctx, query, name))
}

func (s *SQLiteStore) Find(ctx context.Context, filter Filter) ([]*Arcset, error) {
	query := `SELECT id, name, path_regex, label, create_time, rait_type, metadata, status,
	           unit_bytes, segment_bytes, backend, sum_bytes, net_bytes, compress_algo, last_check, comment
	           FROM t_arcset WHERE 1=1`
	var args []any

	if filter.Status != nil {
		query += " AND status = ?"
		args = append(args, *filter.Status)
	}
	query += " ORDER BY name"

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.WrapE(err, "find arcsets")
	}
	defer rows.Close()

	var arcsets []*Arcset
	for rows.Next() {
		a, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		arcsets = append(arcsets, a)
	}
	return arcsets, rows.Err()
}

func (s *SQLiteStore) Update(ctx context.Context, name string, u Update) error {
	query := "UPDATE t_arcset SET "
	var sets []string
	var args []any

	if u.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *u.Status)
	}
	if u.Label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *u.Label)
	}
	if u.Comment != nil {
		sets = append(sets, "comment = ?")
		args = append(args, *u.Comment)
	}
	if u.SumBytes != nil {
		sets = append(sets, "sum_bytes = ?")
		args = append(args, *u.SumBytes)
	}
	if u.NetBytes != nil {
		sets = append(sets, "net_bytes = ?")
		args = append(args, *u.NetBytes)
	}
	if u.LastCheck != nil {
		sets = append(sets, "last_check = ?")
		args = append(args, *u.LastCheck)
	}
	if u.Metadata != nil {
		metadataJSON, _ := json.Marshal(u.Metadata)
		sets = append(sets, "metadata = ?")
		args = append(args, string(metadataJSON))
	}

	if len(sets) == 0 {
		return nil
	}
	query += strings.Join(sets, ", ") + " WHERE name = ?"
	args = append(args, name)

	_, err := s.DB.ExecContext(ctx, query, args...)
	return errors.WrapE(err, "update arcset", "name", name)
}

func (s *SQLiteStore) AddDataset(ctx context.Context, arcsetID, datasetID int) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO r_arcset_dataset (arcset, dataset) VALUES (?, ?)`,
		arcsetID, datasetID)
	return errors.WrapE(err, "add dataset to arcset", "arcset_id", arcsetID, "dataset_id", datasetID)
}

func (s *SQLiteStore) ListDatasetRefs(ctx context.Context, arcsetID int) ([]DatasetRef, error) {
	query := `SELECT d.id, d.name
	           FROM t_dataset d
	           JOIN r_arcset_dataset r ON r.dataset = d.id
	           WHERE r.arcset = ?
	           ORDER BY d.name`
	rows, err := s.DB.QueryContext(ctx, query, arcsetID)
	if err != nil {
		return nil, errors.WrapE(err, "list dataset refs", "arcset_id", arcsetID)
	}
	defer rows.Close()

	var refs []DatasetRef
	for rows.Next() {
		var ref DatasetRef
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, errors.WrapE(err, "scan dataset ref")
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *SQLiteStore) ListArcsetFiles(ctx context.Context, arcsetID int) ([]FileRow, error) {
	query := `SELECT f.id, f.file_path, COALESCE(f.file_size, 0), COALESCE(f.checksum, '')
	           FROM t_file f
	           JOIN r_arcset_dataset r ON r.dataset = f.dataset
	           WHERE r.arcset = ?
	           ORDER BY f.file_path`
	rows, err := s.DB.QueryContext(ctx, query, arcsetID)
	if err != nil {
		return nil, errors.WrapE(err, "list arcset files", "arcset_id", arcsetID)
	}
	defer rows.Close()

	var files []FileRow
	for rows.Next() {
		var f FileRow
		if err := rows.Scan(&f.ID, &f.FilePath, &f.FileSize, &f.Checksum); err != nil {
			return nil, errors.WrapE(err, "scan file row")
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *SQLiteStore) scanOne(row *sql.Row) (*Arcset, error) {
	a := &Arcset{}
	var (
		label, raitType, status, compressAlgo, comment sql.NullString
		createTime, lastCheck                            sql.NullTime
		unitBytes, segmentBytes, sumBytes, netBytes      sql.NullInt64
		metadata                                         []byte
	)
	err := row.Scan(&a.ID, &a.Name, &a.PathRegex, &label, &createTime, &raitType, &metadata, &status,
		&unitBytes, &segmentBytes, &a.Backend, &sumBytes, &netBytes, &compressAlgo, &lastCheck, &comment)
	if err == sql.ErrNoRows {
		return nil, errors.E("arcset not found")
	}
	if err != nil {
		return nil, errors.WrapE(err, "scan arcset row")
	}
	a.Label = label.String
	a.RaitType = raitType.String
	a.Status = status.String
	a.CompressAlgo = compressAlgo.String
	a.Comment = comment.String
	if createTime.Valid {
		a.CreateTime = createTime.Time
	}
	if lastCheck.Valid {
		a.LastCheck = lastCheck.Time
	}
	if unitBytes.Valid {
		a.UnitBytes = unitBytes.Int64
	}
	if segmentBytes.Valid {
		a.SegmentBytes = segmentBytes.Int64
	}
	if sumBytes.Valid {
		a.SumBytes = sumBytes.Int64
	}
	if netBytes.Valid {
		a.NetBytes = netBytes.Int64
	}
	a.Metadata = make(map[string]any)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &a.Metadata); err != nil {
			logrus.Errorf("unmarshal arcset metadata failed: %v", err)
		}
	}
	return a, nil
}

func (s *SQLiteStore) scanRow(rows *sql.Rows) (*Arcset, error) {
	a := &Arcset{}
	var (
		label, raitType, status, compressAlgo, comment sql.NullString
		createTime, lastCheck                            sql.NullTime
		unitBytes, segmentBytes, sumBytes, netBytes      sql.NullInt64
		metadata                                         []byte
	)
	if err := rows.Scan(&a.ID, &a.Name, &a.PathRegex, &label, &createTime, &raitType, &metadata, &status,
		&unitBytes, &segmentBytes, &a.Backend, &sumBytes, &netBytes, &compressAlgo, &lastCheck, &comment); err != nil {
		return nil, errors.WrapE(err, "scan arcset row")
	}
	a.Label = label.String
	a.RaitType = raitType.String
	a.Status = status.String
	a.CompressAlgo = compressAlgo.String
	a.Comment = comment.String
	if createTime.Valid {
		a.CreateTime = createTime.Time
	}
	if lastCheck.Valid {
		a.LastCheck = lastCheck.Time
	}
	if unitBytes.Valid {
		a.UnitBytes = unitBytes.Int64
	}
	if segmentBytes.Valid {
		a.SegmentBytes = segmentBytes.Int64
	}
	if sumBytes.Valid {
		a.SumBytes = sumBytes.Int64
	}
	if netBytes.Valid {
		a.NetBytes = netBytes.Int64
	}
	a.Metadata = make(map[string]any)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &a.Metadata); err != nil {
			logrus.Errorf("unmarshal arcset metadata failed: %v", err)
		}
	}
	return a, nil
}
