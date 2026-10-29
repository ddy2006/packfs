package shard

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kaichao/gopkg/errors"
)

// DatasetStore writes DATA shards into a per-dataset SQLite file.
type DatasetStore struct {
	DB *sql.DB
}

func NewDatasetStore(db *sql.DB) *DatasetStore { return &DatasetStore{DB: db} }

func (s *DatasetStore) CreateShard(ctx context.Context, sh *Shard) error {
	datasetID := int(sh.Dataset.Int64)
	if datasetID <= 0 {
		return errors.E("dataset id is required for dataset shard")
	}

	seq := sh.Seq
	if seq < 0 {
		// Reuse the existing seq for an idempotent re-run of the same shard;
		// otherwise allocate the next local seq.
		err := s.DB.QueryRowContext(ctx,
			`SELECT seq FROM t_shard WHERE dataset = ? AND file_path = ?`,
			datasetID, sh.FilePath).Scan(&seq)
		if err == sql.ErrNoRows {
			if err := s.DB.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(seq), -1) + 1 FROM t_shard WHERE dataset = ?`,
				datasetID).Scan(&seq); err != nil {
				return errors.WrapE(err, "allocate shard seq", "dataset", datasetID)
			}
		} else if err != nil {
			return errors.WrapE(err, "find existing shard seq", "dataset", datasetID, "path", sh.FilePath)
		}
	}
	sh.Seq = seq

	metadataJSON, _ := json.Marshal(sh.Metadata)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_shard (seq, file_path, file_size, type, metadata, sha256, last_check, dataset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(dataset, file_path) DO UPDATE SET
		   seq=excluded.seq, file_size=excluded.file_size, type=excluded.type,
		   metadata=excluded.metadata, sha256=excluded.sha256,
		   last_check=excluded.last_check`,
		sh.Seq, sh.FilePath, sh.FileSize, sh.Type, string(metadataJSON),
		sh.Checksum, nullTimeValue(sh.LastCheck), datasetID)
	if err != nil {
		return errors.WrapE(err, "upsert dataset shard", "file_path", sh.FilePath)
	}

	if err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM t_shard WHERE dataset = ? AND file_path = ?`,
		datasetID, sh.FilePath).Scan(&sh.ID); err != nil {
		return errors.WrapE(err, "query shard id")
	}
	return nil
}

func (s *DatasetStore) ReplaceSegments(ctx context.Context, shardID int, segs []*Segment) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return errors.WrapE(err, "begin tx for replace segments")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM t_segment WHERE shard = ?`, shardID); err != nil {
		return errors.WrapE(err, "delete old segments", "shard_id", shardID)
	}
	for _, seg := range segs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO t_segment (offset, size, csize, shard, file, file_offset, file_size)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			seg.Offset, seg.Size, seg.Csize, shardID, seg.File, seg.FileOffset, seg.FileSize); err != nil {
			return errors.WrapE(err, "insert segment")
		}
	}
	return errors.WrapE(tx.Commit(), "commit segments")
}

func (s *DatasetStore) FindByDataset(ctx context.Context, datasetID int) ([]*Shard, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, seq, file_path, file_size, type, COALESCE(metadata,'{}'),
		        COALESCE(sha256,''), COALESCE(last_check,''), dataset
		 FROM t_shard WHERE dataset = ? ORDER BY seq, file_path`, datasetID)
	if err != nil {
		return nil, errors.WrapE(err, "find dataset shards", "dataset_id", datasetID)
	}
	defer rows.Close()

	var shards []*Shard
	for rows.Next() {
		sh := &Shard{}
		var metadataBytes []byte
		var lastCheck string
		if err := rows.Scan(&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type,
			&metadataBytes, &sh.Checksum, &lastCheck, &sh.Dataset); err != nil {
			return nil, errors.WrapE(err, "scan dataset shard row")
		}
		if lastCheck != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", lastCheck); err == nil {
				sh.LastCheck = t
			}
		}
		sh.Metadata = make(map[string]any)
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &sh.Metadata)
		}
		shards = append(shards, sh)
	}
	return shards, rows.Err()
}

func (s *DatasetStore) FindByFilePath(ctx context.Context, datasetID int, filePath string) (*Shard, error) {
	sh := &Shard{}
	var metadataBytes []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, seq, file_path, file_size, type, COALESCE(metadata,'{}'),
		        COALESCE(sha256,''), dataset
		 FROM t_shard WHERE dataset = ? AND file_path = ?`,
		datasetID, filePath).Scan(
		&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type,
		&metadataBytes, &sh.Checksum, &sh.Dataset)
	if err == sql.ErrNoRows {
		return nil, errors.E("dataset shard not found", "dataset_id", datasetID, "file_path", filePath)
	}
	if err != nil {
		return nil, errors.WrapE(err, "find dataset shard")
	}
	sh.Metadata = make(map[string]any)
	if len(metadataBytes) > 0 {
		_ = json.Unmarshal(metadataBytes, &sh.Metadata)
	}
	return sh, nil
}

func nullTimeValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func (s *DatasetStore) ListUnpackInfo(ctx context.Context, shardID int) ([]UnpackInfo, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.file_path, seg.offset, seg.size, COALESCE(seg.csize, 0)
		 FROM t_segment seg JOIN t_file f ON f.id = seg.file
		 WHERE seg.shard = ? ORDER BY seg.offset`, shardID)
	if err != nil {
		return nil, errors.WrapE(err, "list unpack info", "shard_id", shardID)
	}
	defer rows.Close()
	var infos []UnpackInfo
	for rows.Next() {
		var info UnpackInfo
		if err := rows.Scan(&info.FilePath, &info.Offset, &info.Size, &info.Csize); err != nil {
			return nil, errors.WrapE(err, "scan unpack info")
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}
