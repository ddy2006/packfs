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
	result, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_shard (seq, file_path, file_size, type, checksum, backend, metadata, last_check, arcset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.Seq, sh.FilePath, sh.FileSize, sh.Type, sh.Checksum, sh.Backend, string(metadataJSON), sh.LastCheck, sh.Arcset)
	if err != nil {
		return errors.WrapE(err, "create shard", "file_path", sh.FilePath)
	}
	id, _ := result.LastInsertId()
	sh.ID = int(id)
	return nil
}

func (s *SQLiteStore) AddSegment(ctx context.Context, seg *Segment) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO t_segment (shard_path, offset, size, shard, arcset, compress_algo, checksum, file, file_offset, file_size)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seg.ShardPath, seg.Offset, seg.Size, seg.Shard, seg.Arcset, seg.CompressAlgo, seg.Checksum, seg.File, seg.FileOffset, seg.FileSize)
	return errors.WrapE(err, "add segment", "shard_path", seg.ShardPath)
}

func (s *SQLiteStore) FindByArcset(ctx context.Context, arcsetID int) ([]*Shard, error) {
	query := `SELECT id, COALESCE(seq,0), file_path, COALESCE(file_size,0), COALESCE(type,''),
	           COALESCE(checksum,''), backend, COALESCE(metadata,'{}'),
	           COALESCE(last_check,''), arcset
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
		if err := rows.Scan(&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type, &sh.Checksum, &sh.Backend, &metadataBytes, &lastCheck, &sh.Arcset); err != nil {
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

func (s *SQLiteStore) FindByFilePath(ctx context.Context, filePath string) (*Shard, error) {
	query := `SELECT id, COALESCE(seq,0), file_path, COALESCE(file_size,0), COALESCE(type,''),
	           COALESCE(checksum,''), backend, COALESCE(metadata,'{}'),
	           COALESCE(last_check,''), arcset
	           FROM t_shard WHERE file_path = ?`
	sh := &Shard{}
	var metadataBytes []byte
	var lastCheck string
	err := s.DB.QueryRowContext(ctx, query, filePath).Scan(
		&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type, &sh.Checksum,
		&sh.Backend, &metadataBytes, &lastCheck, &sh.Arcset)
	if err == sql.ErrNoRows {
		return nil, errors.E("shard not found", "file_path", filePath)
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
