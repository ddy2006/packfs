package shard

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

func (s *SQLiteStore) CreateShard(ctx context.Context, sh *Shard) error {
	metadataJSON, _ := json.Marshal(sh.Metadata)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_shard (seq, file_path, file_size, type, sha256, metadata, last_check, arcset, dataset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(arcset, dataset, file_path)
		 DO UPDATE SET seq=excluded.seq, file_size=excluded.file_size, type=excluded.type,
		               sha256=excluded.sha256, metadata=excluded.metadata,
		               last_check=excluded.last_check`,
		sh.Seq, sh.FilePath, sh.FileSize, sh.Type, sh.Checksum,
		string(metadataJSON), sh.LastCheck, sh.Arcset, sh.Dataset)
	if err != nil {
		return errors.WrapE(err, "upsert shard", "file_path", sh.FilePath)
	}

	row := s.DB.QueryRowContext(ctx,
		`SELECT id FROM t_shard WHERE arcset = ? AND dataset = ? AND file_path = ?`,
		sh.Arcset, sh.Dataset, sh.FilePath)
	if err := row.Scan(&sh.ID); err != nil {
		return errors.WrapE(err, "query shard id after upsert")
	}
	return nil
}

func (s *SQLiteStore) ReplaceSegments(ctx context.Context, shardID int, segs []*Segment) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return errors.WrapE(err, "begin tx for replace segments")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM t_segment WHERE shard = ?`, shardID); err != nil {
		return errors.WrapE(err, "delete old segments", "shard_id", shardID)
	}

	for _, seg := range segs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO t_segment (offset, size, shard, file, file_offset, file_size)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			seg.Offset, seg.Size, shardID, seg.File, seg.FileOffset, seg.FileSize)
		if err != nil {
			return errors.WrapE(err, "insert segment")
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) FindByArcset(ctx context.Context, arcsetID int) ([]*Shard, error) {
	query := `SELECT id, COALESCE(seq,0), file_path, COALESCE(file_size,0), COALESCE(type,''),
	           COALESCE(sha256,''), COALESCE(metadata,'{}'),
	           COALESCE(last_check,''), arcset, dataset
	           FROM t_shard WHERE arcset = ? ORDER BY seq`
	rows, err := s.DB.QueryContext(ctx, query, arcsetID)
	if err != nil {
		return nil, errors.WrapE(err, "find shards by arcset", "arcset_id", arcsetID)
	}
	defer rows.Close()

	var shards []*Shard
	for rows.Next() {
		sh := &Shard{}
		var metadataBytes []byte
		var lastCheck string
		if err := rows.Scan(&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type,
			&sh.Checksum, &metadataBytes, &lastCheck, &sh.Arcset, &sh.Dataset); err != nil {
			return nil, errors.WrapE(err, "scan shard row")
		}
		if lastCheck != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", lastCheck); err == nil {
				sh.LastCheck = t
			}
		}
		sh.Metadata = make(map[string]any)
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &sh.Metadata); err != nil {
				logrus.Errorf("unmarshal shard metadata failed: %v", err)
			}
		}
		shards = append(shards, sh)
	}
	return shards, rows.Err()
}

func (s *SQLiteStore) FindByArcsetAndFilePath(ctx context.Context, arcsetID int, filePath string) (*Shard, error) {
	query := `SELECT id, COALESCE(seq,0), file_path, COALESCE(file_size,0), COALESCE(type,''),
	           COALESCE(sha256,''), COALESCE(metadata,'{}'),
	           COALESCE(last_check,''), arcset, dataset
	           FROM t_shard WHERE arcset = ? AND file_path = ?`
	sh := &Shard{}
	var metadataBytes []byte
	var lastCheck string
	err := s.DB.QueryRowContext(ctx, query, arcsetID, filePath).Scan(
		&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type, &sh.Checksum,
		&metadataBytes, &lastCheck, &sh.Arcset, &sh.Dataset)
	if err == sql.ErrNoRows {
		return nil, errors.E("shard not found", "arcset_id", arcsetID, "file_path", filePath)
	}
	if err != nil {
		return nil, errors.WrapE(err, "find shard by file path")
	}
	if lastCheck != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", lastCheck); err == nil {
			sh.LastCheck = t
		}
	}
	sh.Metadata = make(map[string]any)
	if len(metadataBytes) > 0 {
		if err := json.Unmarshal(metadataBytes, &sh.Metadata); err != nil {
			logrus.Errorf("unmarshal shard metadata failed: %v", err)
		}
	}
	return sh, nil
}

func (s *SQLiteStore) ListUnpackInfo(ctx context.Context, shardID int) ([]UnpackInfo, error) {
	query := `SELECT f.file_path, seg.offset, seg.size
	           FROM t_segment seg
	           JOIN t_file f ON f.id = seg.file
	           WHERE seg.shard = ?
	           ORDER BY seg.offset`
	rows, err := s.DB.QueryContext(ctx, query, shardID)
	if err != nil {
		return nil, errors.WrapE(err, "list unpack info", "shard_id", shardID)
	}
	defer rows.Close()

	var infos []UnpackInfo
	for rows.Next() {
		var info UnpackInfo
		if err := rows.Scan(&info.FilePath, &info.Offset, &info.Size); err != nil {
			return nil, errors.WrapE(err, "scan unpack info")
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}
