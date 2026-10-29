package arcset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ShardRow is one row of the global t_shard table.
type ShardRow struct {
	ID           int64
	ArcsetID     int64
	DatasetRef   *int64
	DatasetKey   *string
	Seq          *int
	StripeSeq    *int
	GroupNo      *int
	Pos          *int
	Partition    *int
	FilePath     string
	OriginalName *string
	FileSize     int64
	Type         string
	Metadata     map[string]any
	SHA256       *string
	LastCheck    *time.Time
}

const shardSelect = `SELECT id, arcset, dataset_ref, dataset_key, seq, stripe_seq,
	group_no, pos, partition, file_path, original_name, file_size, type,
	COALESCE(metadata,'{}'::jsonb), sha256, last_check
	FROM t_shard`

func scanShard(row pgx.Row) (*ShardRow, error) {
	sh := &ShardRow{}
	var metadata []byte
	err := row.Scan(&sh.ID, &sh.ArcsetID, &sh.DatasetRef, &sh.DatasetKey, &sh.Seq,
		&sh.StripeSeq, &sh.GroupNo, &sh.Pos, &sh.Partition, &sh.FilePath,
		&sh.OriginalName, &sh.FileSize, &sh.Type, &metadata, &sh.SHA256, &sh.LastCheck)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan shard: %w", err)
	}
	sh.Metadata = map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &sh.Metadata)
	}
	return sh, nil
}

func (s *PGStore) ListShards(ctx context.Context, arcsetID int64) ([]*ShardRow, error) {
	rows, err := s.Pool.Query(ctx, shardSelect+` WHERE arcset = $1 ORDER BY id`, arcsetID)
	if err != nil {
		return nil, fmt.Errorf("list shards: %w", err)
	}
	defer rows.Close()

	var out []*ShardRow
	for rows.Next() {
		sh, err := scanShard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *PGStore) InsertShard(ctx context.Context, sh *ShardRow) error {
	metadata, _ := json.Marshal(sh.Metadata)
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO t_shard
		   (arcset, dataset_ref, dataset_key, seq, stripe_seq, group_no, pos,
		    partition, file_path, original_name, file_size, type, metadata, sha256, last_check)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15)
		 RETURNING id`,
		sh.ArcsetID, sh.DatasetRef, sh.DatasetKey, sh.Seq, sh.StripeSeq, sh.GroupNo,
		sh.Pos, sh.Partition, sh.FilePath, sh.OriginalName, sh.FileSize, sh.Type,
		string(metadata), sh.SHA256, sh.LastCheck).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert shard %s: %w", sh.FilePath, err)
	}
	sh.ID = id
	return nil
}

func (s *PGStore) UpdateShardData(ctx context.Context, sh *ShardRow) error {
	metadata, _ := json.Marshal(sh.Metadata)
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_shard SET dataset_ref=$2, dataset_key=$3, seq=$4, stripe_seq=$5,
		 group_no=$6, pos=$7, partition=$8, file_path=$9, original_name=$10,
		 file_size=$11, type=$12, metadata=$13::jsonb, sha256=$14
		 WHERE id=$1`,
		sh.ID, sh.DatasetRef, sh.DatasetKey, sh.Seq, sh.StripeSeq, sh.GroupNo,
		sh.Pos, sh.Partition, sh.FilePath, sh.OriginalName, sh.FileSize, sh.Type,
		string(metadata), sh.SHA256)
	if err != nil {
		return fmt.Errorf("update shard %s: %w", sh.FilePath, err)
	}
	return nil
}

func (s *PGStore) DeleteShard(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM t_shard WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete shard %d: %w", id, err)
	}
	return nil
}

func (s *PGStore) DeleteECShards(ctx context.Context, arcsetID int64, groupNo, stripeSeq int) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM t_shard WHERE arcset=$1 AND group_no=$2 AND stripe_seq=$3
		 AND type = 'EC'`, arcsetID, groupNo, stripeSeq)
	if err != nil {
		return fmt.Errorf("delete EC/PAD shards: %w", err)
	}
	return nil
}

func (s *PGStore) SetParityDegraded(ctx context.Context, arcsetID int64, degraded bool) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_arcset SET parity_degraded=$2 WHERE id=$1`, arcsetID, degraded)
	if err != nil {
		return fmt.Errorf("set parity_degraded: %w", err)
	}
	return nil
}

func (s *PGStore) DeleteECAndPADShards(ctx context.Context, arcsetID int64, groupNo, stripeSeq int) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM t_shard WHERE arcset=$1 AND group_no=$2 AND stripe_seq=$3
		 AND type IN ('EC','PAD')`, arcsetID, groupNo, stripeSeq)
	if err != nil {
		return fmt.Errorf("delete EC/PAD shards: %w", err)
	}
	return nil
}

func (s *PGStore) UpdateShardMetadata(ctx context.Context, id int64, metadata map[string]any) error {
	raw, _ := json.Marshal(metadata)
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_shard SET metadata=$2::jsonb WHERE id=$1`, id, string(raw))
	if err != nil {
		return fmt.Errorf("update shard metadata: %w", err)
	}
	return nil
}

func (s *PGStore) SetBuildPhase(ctx context.Context, arcsetID int64, phase string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_arcset SET status='building', build_phase=$2 WHERE id=$1`,
		arcsetID, phase)
	if err != nil {
		return fmt.Errorf("set build_phase: %w", err)
	}
	return nil
}

func (s *PGStore) CompleteBuild(ctx context.Context, arcsetID int64) error {
	now := time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_arcset SET status='completed', build_phase='none', health='ok',
		 parity_degraded=false, last_check=$2 WHERE id=$1`, arcsetID, now)
	if err != nil {
		return fmt.Errorf("complete arcset build: %w", err)
	}
	return nil
}

func (s *PGStore) FindShardByPath(ctx context.Context, arcsetID int64, filePath string) (*ShardRow, error) {
	return scanShard(s.Pool.QueryRow(ctx, shardSelect+` WHERE arcset=$1 AND file_path=$2`, arcsetID, filePath))
}

func (s *PGStore) SetHealth(ctx context.Context, arcsetID int64, health string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE t_arcset SET health=$2, last_check=now() WHERE id=$1`,
		arcsetID, health)
	if err != nil {
		return fmt.Errorf("set arcset health: %w", err)
	}
	return nil
}

func (s *PGStore) UpdateShardLastCheck(ctx context.Context, shardID int64, t time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE t_shard SET last_check=$2 WHERE id=$1`, shardID, t)
	if err != nil {
		return fmt.Errorf("update shard last_check: %w", err)
	}
	return nil
}
