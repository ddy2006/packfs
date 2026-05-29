CREATE TABLE "T_arcset" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" VARCHAR NOT NULL,
  "label" VARCHAR,
  "metadata" JSON,
  "status" VARCHAR,
  "current_path" VARCHAR,
  "last_check" DATETIME,
  "comment" TEXT
);

CREATE TABLE "T_dataset" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" VARCHAR,
  "label" VARCHAR,
  "metadata" JSON NOT NULL,
  "status" TEXT,
  "current_path" VARCHAR,
  "comment" TEXT
);

CREATE TABLE "R_arcset_dataset" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "arcset" INTEGER NOT NULL REFERENCES "T_arcset" ("id") ON DELETE CASCADE,
  "dataset" INTEGER NOT NULL REFERENCES "T_dataset" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_r_arcset_dataset__arcset" ON "R_arcset_dataset" ("arcset");

CREATE INDEX "idx_r_arcset_dataset__dataset" ON "R_arcset_dataset" ("dataset");

CREATE TABLE "T_file" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "file_path" VARCHAR NOT NULL,
  "file_size" BIGINT,
  "metadata" JSON,
  "sha256" TEXT,
  "dataset" INTEGER NOT NULL REFERENCES "T_dataset" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_t_file__dataset" ON "T_file" ("dataset");

CREATE TABLE "T_fs_mount" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "mount_point" VARCHAR NOT NULL,
  "type" TEXT NOT NULL,
  "status" BOOLEAN,
  "arcset" INTEGER NOT NULL REFERENCES "T_arcset" ("id") ON DELETE CASCADE,
  "data_path" VARCHAR
);

CREATE INDEX "idx_t_fs_mount__arcset" ON "T_fs_mount" ("arcset");

CREATE TABLE "T_shard" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "seq" INTEGER,
  "file_path" TEXT,
  "file_size" BIGINT,
  "type" VARCHAR,
  "metadata" JSON,
  "sha256" VARCHAR,
  "last_check" DATETIME,
  "arcset" INTEGER NOT NULL REFERENCES "T_arcset" ("id") ON DELETE CASCADE,
  "dataset" INTEGER NOT NULL REFERENCES "T_dataset" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_t_shard__arcset" ON "T_shard" ("arcset");

CREATE INDEX "idx_t_shard__dataset" ON "T_shard" ("dataset");

CREATE TABLE "T_segment" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "offset" BIGINT,
  "size" BIGINT,
  "csize" BIGINT,
  "shard" INTEGER NOT NULL REFERENCES "T_shard" ("id") ON DELETE CASCADE,
  "file" INTEGER NOT NULL REFERENCES "T_file" ("id") ON DELETE CASCADE,
  "file_offset" BIGINT,
  "file_size" BIGINT
);

CREATE INDEX "idx_t_segment__file" ON "T_segment" ("file");

CREATE INDEX "idx_t_segment__shard" ON "T_segment" ("shard");

CREATE TABLE "T_tape" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "bar_code" TEXT NOT NULL,
  "spec" JSON,
  "location" TEXT,
  "status" TEXT,
  "last_access" DATETIME,
  "comment" TEXT
);

CREATE TABLE "R_shard_tape" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "shard" INTEGER NOT NULL REFERENCES "T_shard" ("id") ON DELETE CASCADE,
  "tape" INTEGER NOT NULL REFERENCES "T_tape" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_r_shard_tape__shard" ON "R_shard_tape" ("shard");

CREATE INDEX "idx_r_shard_tape__tape" ON "R_shard_tape" ("tape");

CREATE UNIQUE INDEX "idx_t_shard__arcset_dataset_file_path"
  ON "T_shard" ("arcset", "dataset", "file_path");