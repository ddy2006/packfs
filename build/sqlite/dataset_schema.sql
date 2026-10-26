-- Per-dataset SQLite schema.
-- One file per dataset: <dataset_uid>.db
-- Contains dataset/file/segment/DATA-shard metadata. No arcset tables.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS t_dataset (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'building'
    CHECK (status IN ('building','built')),
  metadata JSON NOT NULL DEFAULT '{}',
  current_path TEXT,
  comment TEXT
);

CREATE TABLE IF NOT EXISTS t_file (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_path TEXT NOT NULL,
  file_size BIGINT,
  metadata JSON,
  sha256 TEXT,
  dataset INTEGER NOT NULL REFERENCES t_dataset(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_t_file__dataset ON t_file(dataset);

CREATE TABLE IF NOT EXISTS t_shard (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seq INTEGER NOT NULL,
  file_path TEXT NOT NULL,
  file_size BIGINT NOT NULL DEFAULT 0,
  type TEXT NOT NULL DEFAULT 'DATA' CHECK (type = 'DATA'),
  metadata JSON,
  sha256 TEXT,
  last_check DATETIME,
  dataset INTEGER NOT NULL REFERENCES t_dataset(id) ON DELETE CASCADE,
  UNIQUE (dataset, seq),
  UNIQUE (dataset, file_path)
);
CREATE INDEX IF NOT EXISTS idx_t_shard__dataset ON t_shard(dataset);

CREATE TABLE IF NOT EXISTS t_segment (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  offset BIGINT,
  size BIGINT,
  csize BIGINT,
  shard INTEGER NOT NULL REFERENCES t_shard(id) ON DELETE CASCADE,
  file INTEGER NOT NULL REFERENCES t_file(id) ON DELETE CASCADE,
  file_offset BIGINT,
  file_size BIGINT
);
CREATE INDEX IF NOT EXISTS idx_t_segment__file ON t_segment(file);
CREATE INDEX IF NOT EXISTS idx_t_segment__shard ON t_segment(shard);
