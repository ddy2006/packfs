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
  "type" VARCHAR NOT NULL,
  "status" BOOLEAN,
  "data_path" VARCHAR,
  "dataset" INTEGER NOT NULL REFERENCES "T_dataset" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_t_fs_mount__dataset" ON "T_fs_mount" ("dataset");

CREATE TABLE "T_shard" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "seq" INTEGER,
  "file_path" TEXT DEFAULT VARCHAR,
  "file_size" BIGINT,
  "type" VARCHAR,
  "metadata" JSON,
  "sha256" VARCHAR,
  "last_check" DATETIME,
  "arcset" INTEGER REFERENCES "T_arcset" ("id") ON DELETE SET NULL,
  "dataset" INTEGER REFERENCES "T_dataset" ("id") ON DELETE SET NULL
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