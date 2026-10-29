-- Keep in sync with build/postgres/arcset_schema.sql.
-- Global PostgreSQL schema for arcset metadata.
-- dataset-level metadata lives in per-dataset SQLite files.
-- No cross-database foreign keys; dataset_uid + seq is the logical link.

CREATE TABLE IF NOT EXISTS t_dataset_ref (
  id BIGSERIAL PRIMARY KEY,
  uid UUID NOT NULL UNIQUE,
  name TEXT NOT NULL,
  sqlite_path TEXT NOT NULL UNIQUE,
  metadata JSONB NOT NULL DEFAULT '{}',
  create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
  comment TEXT
);

CREATE TABLE IF NOT EXISTS t_arcset (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  label TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'defined'
    CHECK (status IN ('defined','building','completed')),
  build_phase TEXT NOT NULL DEFAULT 'none'
    CHECK (build_phase IN ('data','ec','none')),
  health TEXT NOT NULL DEFAULT 'ok'
    CHECK (health IN ('ok','degraded','failed')),
  parity_degraded BOOLEAN NOT NULL DEFAULT false,
  current_path TEXT,
  last_check TIMESTAMPTZ,
  comment TEXT
);

CREATE INDEX IF NOT EXISTS idx_t_arcset_status ON t_arcset(status);
CREATE INDEX IF NOT EXISTS idx_t_arcset_health ON t_arcset(health);

CREATE TABLE IF NOT EXISTS r_arcset_dataset_ref (
  id BIGSERIAL PRIMARY KEY,
  arcset BIGINT NOT NULL REFERENCES t_arcset(id) ON DELETE CASCADE,
  dataset_ref BIGINT NOT NULL REFERENCES t_dataset_ref(id) ON DELETE CASCADE,
  dataset_key TEXT NOT NULL,
  append_order INTEGER NOT NULL,
  UNIQUE (dataset_ref),
  UNIQUE (arcset, dataset_key),
  UNIQUE (arcset, append_order)
);

CREATE INDEX IF NOT EXISTS idx_r_arcset_dataset_ref__dataset_ref
  ON r_arcset_dataset_ref(dataset_ref);

CREATE TABLE IF NOT EXISTS t_shard (
  id BIGSERIAL PRIMARY KEY,
  arcset BIGINT NOT NULL REFERENCES t_arcset(id) ON DELETE CASCADE,
  dataset_ref BIGINT REFERENCES t_dataset_ref(id) ON DELETE CASCADE,
  dataset_key TEXT,
  seq INTEGER,
  stripe_seq INTEGER,
  group_no INTEGER,
  pos INTEGER,
  partition INTEGER,
  file_path TEXT NOT NULL,
  original_name TEXT,
  file_size BIGINT NOT NULL DEFAULT 0,
  type TEXT NOT NULL CHECK (type IN ('DATA','EC','PAD')),
  metadata JSONB,
  sha256 TEXT,
  last_check TIMESTAMPTZ,
  CHECK (
    (type = 'DATA' AND dataset_ref IS NOT NULL) OR
    (type IN ('EC','PAD') AND dataset_ref IS NULL)
  )
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_t_shard_arcset_path
  ON t_shard(arcset, file_path);
CREATE UNIQUE INDEX IF NOT EXISTS ux_t_shard_stripe_pos
  ON t_shard(arcset, group_no, stripe_seq, pos);
CREATE UNIQUE INDEX IF NOT EXISTS ux_t_shard_partition_seq
  ON t_shard(arcset, partition, stripe_seq);
CREATE UNIQUE INDEX IF NOT EXISTS ux_t_shard_data_seq
  ON t_shard(arcset, dataset_ref, seq) WHERE type = 'DATA';
CREATE INDEX IF NOT EXISTS idx_t_shard_arcset ON t_shard(arcset);
CREATE INDEX IF NOT EXISTS idx_t_shard_dataset_ref ON t_shard(dataset_ref);
CREATE INDEX IF NOT EXISTS idx_t_shard_partition ON t_shard(arcset, partition);

CREATE TABLE IF NOT EXISTS t_fs_mount (
  id BIGSERIAL PRIMARY KEY,
  mount_point TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL CHECK (type IN ('dataset','arcset')),
  dataset_uid UUID,
  arcset BIGINT REFERENCES t_arcset(id) ON DELETE CASCADE,
  create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    (type = 'dataset' AND dataset_uid IS NOT NULL AND arcset IS NULL) OR
    (type = 'arcset'  AND arcset IS NOT NULL AND dataset_uid IS NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_t_fs_mount__dataset_uid ON t_fs_mount(dataset_uid);
CREATE INDEX IF NOT EXISTS idx_t_fs_mount__arcset ON t_fs_mount(arcset);
