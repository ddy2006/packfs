package arcset

import (
	"context"
	"fmt"
	"time"
)

// MountRow is a t_fs_mount record.
type MountRow struct {
	ID         int64
	MountPoint string
	Type       string
	DatasetUID *string
	ArcsetID   *int64
	CreateTime time.Time
}

func (s *PGStore) AddMount(ctx context.Context, mountPoint, typ string, datasetUID *string, arcsetID *int64) (*MountRow, error) {
	var row MountRow
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO t_fs_mount (mount_point, type, dataset_uid, arcset)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, mount_point, type, dataset_uid::text, arcset, create_time`,
		mountPoint, typ, datasetUID, arcsetID).Scan(
		&row.ID, &row.MountPoint, &row.Type, &row.DatasetUID, &row.ArcsetID, &row.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("add fs mount: %w", err)
	}
	return &row, nil
}

func (s *PGStore) ListMounts(ctx context.Context) ([]*MountRow, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, mount_point, type, dataset_uid::text, arcset, create_time
		 FROM t_fs_mount ORDER BY mount_point`)
	if err != nil {
		return nil, fmt.Errorf("list fs mounts: %w", err)
	}
	defer rows.Close()
	var out []*MountRow
	for rows.Next() {
		row := &MountRow{}
		if err := rows.Scan(&row.ID, &row.MountPoint, &row.Type, &row.DatasetUID,
			&row.ArcsetID, &row.CreateTime); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *PGStore) DeleteMount(ctx context.Context, mountPoint string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM t_fs_mount WHERE mount_point=$1`, mountPoint)
	if err != nil {
		return fmt.Errorf("delete fs mount: %w", err)
	}
	return nil
}
