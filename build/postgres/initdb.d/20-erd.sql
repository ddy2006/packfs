CREATE TABLE "t_arcset" (
  "id" SERIAL PRIMARY KEY,
  "name" VARCHAR NOT NULL,
  "path_regex" VARCHAR NOT NULL,
  "label" VARCHAR,
  "create_time" TIMESTAMP,
  "rait_type" VARCHAR,
  "metadata" JSONB,
  "status" VARCHAR,
  "unit_bytes" BIGINT,
  "segment_bytes" BIGINT,
  "backend" VARCHAR NOT NULL,
  "sum_bytes" BIGINT,
  "net_bytes" BIGINT,
  "compress_algo" TEXT DEFAULT VARCHAT,
  "last_check" TIMESTAMP,
  "comment" TEXT
);

CREATE TABLE "t_dataset" (
  "id" SERIAL PRIMARY KEY,
  "name" VARCHAR,
  "relative_path" VARCHAR NOT NULL,
  "label" VARCHAR,
  "metadata" JSONB NOT NULL
);

CREATE TABLE "r_arcset_dataset" (
  "id" SERIAL PRIMARY KEY,
  "arcset" INTEGER NOT NULL,
  "dataset" INTEGER NOT NULL
);

CREATE INDEX "idx_r_arcset_dataset__arcset" ON "r_arcset_dataset" ("arcset");

CREATE INDEX "idx_r_arcset_dataset__dataset" ON "r_arcset_dataset" ("dataset");

ALTER TABLE "r_arcset_dataset" ADD CONSTRAINT "fk_r_arcset_dataset__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE CASCADE;

ALTER TABLE "r_arcset_dataset" ADD CONSTRAINT "fk_r_arcset_dataset__dataset" FOREIGN KEY ("dataset") REFERENCES "t_dataset" ("id") ON DELETE CASCADE;

CREATE TABLE "t_file" (
  "id" SERIAL PRIMARY KEY,
  "file_path" VARCHAR NOT NULL,
  "file_size" BIGINT,
  "metadata" JSONB,
  "ctime" TIMESTAMP,
  "mtime" TIMESTAMP,
  "checksum" TEXT,
  "dataset" INTEGER
);

CREATE INDEX "idx_t_file__dataset" ON "t_file" ("dataset");

ALTER TABLE "t_file" ADD CONSTRAINT "fk_t_file__dataset" FOREIGN KEY ("dataset") REFERENCES "t_dataset" ("id") ON DELETE SET NULL;

CREATE TABLE "t_fs_mount" (
  "id" SERIAL PRIMARY KEY,
  "mount_point" VARCHAR NOT NULL,
  "type" TEXT NOT NULL,
  "status" BOOLEAN,
  "arcset" INTEGER NOT NULL,
  "data_path" VARCHAR
);

CREATE INDEX "idx_t_fs_mount__arcset" ON "t_fs_mount" ("arcset");

ALTER TABLE "t_fs_mount" ADD CONSTRAINT "fk_t_fs_mount__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE CASCADE;

CREATE TABLE "t_shard" (
  "id" SERIAL PRIMARY KEY,
  "seq" SMALLINT,
  "file_path" TEXT,
  "file_size" BIGINT,
  "type" VARCHAR,
  "checksum" VARCHAR,
  "backend" VARCHAR NOT NULL,
  "metadata" JSONB,
  "last_check" TIMESTAMP,
  "arcset" INTEGER
);

CREATE INDEX "idx_t_shard__arcset" ON "t_shard" ("arcset");

ALTER TABLE "t_shard" ADD CONSTRAINT "fk_t_shard__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE SET NULL;

CREATE TABLE "t_segment" (
  "id" SERIAL PRIMARY KEY,
  "shard_path" VARCHAR NOT NULL,
  "offset" BIGINT,
  "size" BIGINT,
  "shard" INTEGER NOT NULL,
  "arcset" INTEGER NOT NULL,
  "compress_algo" VARCHAR NOT NULL,
  "checksum" VARCHAR,
  "file" INTEGER NOT NULL,
  "file_offset" BIGINT,
  "file_size" BIGINT
);

CREATE INDEX "idx_t_segment__arcset" ON "t_segment" ("arcset");

CREATE INDEX "idx_t_segment__file" ON "t_segment" ("file");

CREATE INDEX "idx_t_segment__shard" ON "t_segment" ("shard");

ALTER TABLE "t_segment" ADD CONSTRAINT "fk_t_segment__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE CASCADE;

ALTER TABLE "t_segment" ADD CONSTRAINT "fk_t_segment__file" FOREIGN KEY ("file") REFERENCES "t_file" ("id") ON DELETE CASCADE;

ALTER TABLE "t_segment" ADD CONSTRAINT "fk_t_segment__shard" FOREIGN KEY ("shard") REFERENCES "t_shard" ("id") ON DELETE CASCADE;

CREATE TABLE "t_tape" (
  "id" SERIAL PRIMARY KEY,
  "arcset" INTEGER NOT NULL,
  "bar_code" TEXT NOT NULL,
  "spec" JSONB,
  "location" TEXT,
  "comment" TEXT
);

CREATE INDEX "idx_t_tape__arcset" ON "t_tape" ("arcset");

ALTER TABLE "t_tape" ADD CONSTRAINT "fk_t_tape__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE CASCADE;