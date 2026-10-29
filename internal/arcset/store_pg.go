package arcset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore persists arcset-level metadata in the global PostgreSQL database.
type PGStore struct {
	Pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{Pool: pool} }

// CreateParams describes a new arcset.
type CreateParams struct {
	Name        string
	Label       string
	CurrentPath string
	Metadata    map[string]any
	Comment     string
}

// DatasetRefRow is the global identity of one dataset.
type DatasetRefRow struct {
	ID         int64
	UID        string
	Name       string
	SQLitePath string
	Metadata   map[string]any
}

// Relation links one dataset to one arcset.
type Relation struct {
	ID          int64
	ArcsetID    int64
	DatasetRef  int64
	DatasetKey  string
	AppendOrder int
}

// AppendDatasetParams describes a dataset registration.
type AppendDatasetParams struct {
	ArcsetID        int64
	UID             string
	Name            string
	SQLitePath      string
	DatasetKey      string
	DatasetMetadata map[string]any
}

func (s *PGStore) Create(ctx context.Context, p CreateParams) (*Arcset, error) {
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	if _, ok := p.Metadata["create_time"]; !ok {
		p.Metadata["create_time"] = time.Now().Format(time.RFC3339)
	}
	metadataJSON, _ := json.Marshal(p.Metadata)

	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO t_arcset
		   (name, label, metadata, status, build_phase, health, parity_degraded,
		    current_path, comment)
		 VALUES ($1, NULLIF($2,''), $3::jsonb, 'defined', 'none', 'ok', false,
		         NULLIF($4,''), NULLIF($5,''))
		 RETURNING id`,
		p.Name, p.Label, string(metadataJSON), p.CurrentPath, p.Comment).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create arcset %q: %w", p.Name, err)
	}
	return s.FindByID(ctx, id)
}

func (s *PGStore) FindByID(ctx context.Context, id int64) (*Arcset, error) {
	return s.scanArcset(s.Pool.QueryRow(ctx, arcsetSelect+` WHERE id = $1`, id))
}

func (s *PGStore) FindByName(ctx context.Context, name string) (*Arcset, error) {
	return s.scanArcset(s.Pool.QueryRow(ctx, arcsetSelect+` WHERE name = $1`, name))
}

func (s *PGStore) UpdateMetadata(ctx context.Context, id int64, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	_, err := s.Pool.Exec(ctx,
		`UPDATE t_arcset SET metadata = $1::jsonb WHERE id = $2`, string(metadataJSON), id)
	if err != nil {
		return fmt.Errorf("update arcset metadata: %w", err)
	}
	return nil
}

func (s *PGStore) List(ctx context.Context, status string) ([]*Arcset, error) {
	query := arcsetSelect
	var args []any
	if status != "" {
		query += ` WHERE status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY name`
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list arcsets: %w", err)
	}
	defer rows.Close()

	var out []*Arcset
	for rows.Next() {
		a, err := scanArcsetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const arcsetSelect = `SELECT id, name, COALESCE(label,''), COALESCE(metadata,'{}'::jsonb),
	status, build_phase, health, parity_degraded, COALESCE(current_path,''),
	last_check, COALESCE(comment,'')
	FROM t_arcset`

func (s *PGStore) scanArcset(row pgx.Row) (*Arcset, error) {
	a := &Arcset{}
	var metadata []byte
	var lastCheck *time.Time
	err := row.Scan(&a.ID, &a.Name, &a.Label, &metadata, &a.Status, &a.BuildPhase,
		&a.Health, &a.ParityDegraded, &a.CurrentPath, &lastCheck, &a.Comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("arcset not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan arcset: %w", err)
	}
	if lastCheck != nil {
		a.LastCheck = *lastCheck
	}
	a.Metadata = map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &a.Metadata)
	}
	return a, nil
}

func scanArcsetRow(rows pgx.Rows) (*Arcset, error) {
	a := &Arcset{}
	var metadata []byte
	var lastCheck *time.Time
	if err := rows.Scan(&a.ID, &a.Name, &a.Label, &metadata, &a.Status, &a.BuildPhase,
		&a.Health, &a.ParityDegraded, &a.CurrentPath, &lastCheck, &a.Comment); err != nil {
		return nil, fmt.Errorf("scan arcset: %w", err)
	}
	if lastCheck != nil {
		a.LastCheck = *lastCheck
	}
	a.Metadata = map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &a.Metadata)
	}
	return a, nil
}

// AppendDataset registers a dataset in PG and sets the arcset to building/data.
// It is idempotent for the same dataset/arcset pair.
func (s *PGStore) AppendDataset(ctx context.Context, p AppendDatasetParams) (*Relation, error) {
	if p.DatasetKey == "" {
		return nil, fmt.Errorf("dataset_key is required")
	}
	if p.DatasetMetadata == nil {
		p.DatasetMetadata = map[string]any{}
	}
	dsMeta, _ := json.Marshal(p.DatasetMetadata)

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin append tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var status, phase, health string
	err = tx.QueryRow(ctx,
		`SELECT status, build_phase, health FROM t_arcset WHERE id = $1 FOR UPDATE`,
		p.ArcsetID).Scan(&status, &phase, &health)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("arcset %d not found", p.ArcsetID)
	}
	if err != nil {
		return nil, fmt.Errorf("load arcset: %w", err)
	}
	if health != "ok" {
		return nil, fmt.Errorf("arcset health is %s", health)
	}
	if status == "building" && phase == "ec" {
		return nil, fmt.Errorf("arcset is busy in build_phase=ec")
	}

	var datasetRefID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO t_dataset_ref (uid, name, sqlite_path, metadata)
		 VALUES ($1::uuid, $2, $3, $4::jsonb)
		 ON CONFLICT (uid) DO UPDATE SET name = EXCLUDED.name, sqlite_path = EXCLUDED.sqlite_path
		 RETURNING id`,
		p.UID, p.Name, p.SQLitePath, string(dsMeta)).Scan(&datasetRefID)
	if err != nil {
		return nil, fmt.Errorf("upsert dataset_ref: %w", err)
	}

	var rel Relation
	rel.ArcsetID = p.ArcsetID
	rel.DatasetRef = datasetRefID
	rel.DatasetKey = p.DatasetKey
	err = tx.QueryRow(ctx,
		`SELECT id, arcset, dataset_ref, dataset_key, append_order
		 FROM r_arcset_dataset_ref WHERE dataset_ref = $1`, datasetRefID).Scan(
		&rel.ID, &rel.ArcsetID, &rel.DatasetRef, &rel.DatasetKey, &rel.AppendOrder)
	if err == nil {
		if rel.ArcsetID != p.ArcsetID {
			return nil, fmt.Errorf("dataset is already registered in arcset %d", rel.ArcsetID)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &rel, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("find existing relation: %w", err)
	}

	var nextOrder int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(append_order), -1) + 1 FROM r_arcset_dataset_ref WHERE arcset = $1`,
		p.ArcsetID).Scan(&nextOrder); err != nil {
		return nil, fmt.Errorf("allocate append_order: %w", err)
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO r_arcset_dataset_ref (arcset, dataset_ref, dataset_key, append_order)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		p.ArcsetID, datasetRefID, p.DatasetKey, nextOrder).Scan(&rel.ID)
	if err != nil {
		return nil, fmt.Errorf("insert relation: %w", err)
	}
	rel.AppendOrder = nextOrder

	if _, err := tx.Exec(ctx,
		`UPDATE t_arcset SET status='building', build_phase='data' WHERE id=$1`,
		p.ArcsetID); err != nil {
		return nil, fmt.Errorf("update arcset state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &rel, nil
}

// RelationDatasetRow is a relation joined with its global dataset identity.
type RelationDatasetRow struct {
	Relation
	UID        string
	Name       string
	SQLitePath string
	Metadata   map[string]any
}

func (s *PGStore) ListRelations(ctx context.Context, arcsetID int64) ([]RelationDatasetRow, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT r.id, r.arcset, r.dataset_ref, r.dataset_key, r.append_order,
		        d.uid::text, d.name, d.sqlite_path, COALESCE(d.metadata,'{}'::jsonb)
		 FROM r_arcset_dataset_ref r
		 JOIN t_dataset_ref d ON d.id = r.dataset_ref
		 WHERE r.arcset = $1
		 ORDER BY r.append_order`, arcsetID)
	if err != nil {
		return nil, fmt.Errorf("list relations: %w", err)
	}
	defer rows.Close()

	var out []RelationDatasetRow
	for rows.Next() {
		var row RelationDatasetRow
		var metadata []byte
		if err := rows.Scan(&row.ID, &row.ArcsetID, &row.DatasetRef, &row.DatasetKey,
			&row.AppendOrder, &row.UID, &row.Name, &row.SQLitePath, &metadata); err != nil {
			return nil, fmt.Errorf("scan relation: %w", err)
		}
		row.Metadata = map[string]any{}
		_ = json.Unmarshal(metadata, &row.Metadata)
		out = append(out, row)
	}
	return out, rows.Err()
}

// FindRelationByDatasetUID returns the relation for a dataset uid.
func (s *PGStore) FindRelationByDatasetUID(ctx context.Context, uid string) (*Relation, *DatasetRefRow, error) {
	var rel Relation
	var ref DatasetRefRow
	var metadata []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT r.id, r.arcset, r.dataset_ref, r.dataset_key, r.append_order,
		        d.uid::text, d.name, d.sqlite_path, COALESCE(d.metadata,'{}'::jsonb)
		 FROM r_arcset_dataset_ref r
		 JOIN t_dataset_ref d ON d.id = r.dataset_ref
		 WHERE d.uid = $1::uuid`, uid).Scan(
		&rel.ID, &rel.ArcsetID, &rel.DatasetRef, &rel.DatasetKey, &rel.AppendOrder,
		&ref.UID, &ref.Name, &ref.SQLitePath, &metadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find relation by uid: %w", err)
	}
	ref.ID = rel.DatasetRef
	ref.Metadata = map[string]any{}
	_ = json.Unmarshal(metadata, &ref.Metadata)
	return &rel, &ref, nil
}
