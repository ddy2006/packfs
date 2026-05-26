package shard

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
	query := `SELECT id, seq, file_path, file_size, type, checksum, backend, metadata, last_check, arcset
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
		if err := rows.Scan(&sh.ID, &sh.Seq, &sh.FilePath, &sh.FileSize, &sh.Type, &sh.Checksum, &sh.Backend, &metadataBytes, &sh.LastCheck, &sh.Arcset); err != nil {
			return nil, errors.WrapE(err, "scan shard row")
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
