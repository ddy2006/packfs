CREATE TABLE "t_arcset" (
  "id" SERIAL PRIMARY KEY,
  "name" VARCHAR NOT NULL,
  "label" VARCHAR,
  "metadata" JSONB,
  "status" VARCHAR,
  "current_path" VARCHAR,
  "last_check" TIMESTAMP,
  "comment" TEXT
);

CREATE TABLE "t_dataset" (
  "id" SERIAL PRIMARY KEY,
  "name" VARCHAR,
  "label" VARCHAR,
  "metadata" JSONB NOT NULL,
  "status" TEXT,
  "current_path" VARCHAR,
  "comment" TEXT
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
  "sha256" TEXT,
  "dataset" INTEGER NOT NULL
);

CREATE INDEX "idx_t_file__dataset" ON "t_file" ("dataset");

ALTER TABLE "t_file" ADD CONSTRAINT "fk_t_file__dataset" FOREIGN KEY ("dataset") REFERENCES "t_dataset" ("id") ON DELETE CASCADE;

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
  "metadata" JSONB,
  "sha256" VARCHAR,
  "last_check" TIMESTAMP,
  "arcset" INTEGER NOT NULL,
  "dataset" INTEGER NOT NULL
);

CREATE INDEX "idx_t_shard__arcset" ON "t_shard" ("arcset");

CREATE INDEX "idx_t_shard__dataset" ON "t_shard" ("dataset");

ALTER TABLE "t_shard" ADD CONSTRAINT "fk_t_shard__arcset" FOREIGN KEY ("arcset") REFERENCES "t_arcset" ("id") ON DELETE CASCADE;

ALTER TABLE "t_shard" ADD CONSTRAINT "fk_t_shard__dataset" FOREIGN KEY ("dataset") REFERENCES "t_dataset" ("id") ON DELETE CASCADE;

CREATE TABLE "t_segment" (
  "id" SERIAL PRIMARY KEY,
  "offset" BIGINT,
  "size" BIGINT,
  "shard" INTEGER NOT NULL,
  "file" INTEGER NOT NULL,
  "file_offset" BIGINT,
  "file_size" BIGINT
);

CREATE INDEX "idx_t_segment__file" ON "t_segment" ("file");

CREATE INDEX "idx_t_segment__shard" ON "t_segment" ("shard");

ALTER TABLE "t_segment" ADD CONSTRAINT "fk_t_segment__file" FOREIGN KEY ("file") REFERENCES "t_file" ("id") ON DELETE CASCADE;

ALTER TABLE "t_segment" ADD CONSTRAINT "fk_t_segment__shard" FOREIGN KEY ("shard") REFERENCES "t_shard" ("id") ON DELETE CASCADE;

CREATE TABLE "t_tape" (
  "id" SERIAL PRIMARY KEY,
  "bar_code" TEXT NOT NULL,
  "spec" JSONB,
  "location" TEXT,
  "status" TEXT,
  "last_access" TIMESTAMP,
  "comment" TEXT
);

CREATE TABLE "r_shard_tape" (
  "id" SERIAL PRIMARY KEY,
  "shard" INTEGER NOT NULL,
  "tape" INTEGER NOT NULL
);

CREATE INDEX "idx_r_shard_tape__shard" ON "r_shard_tape" ("shard");

CREATE INDEX "idx_r_shard_tape__tape" ON "r_shard_tape" ("tape");

ALTER TABLE "r_shard_tape" ADD CONSTRAINT "fk_r_shard_tape__shard" FOREIGN KEY ("shard") REFERENCES "t_shard" ("id") ON DELETE CASCADE;

ALTER TABLE "r_shard_tape" ADD CONSTRAINT "fk_r_shard_tape__tape" FOREIGN KEY ("tape") REFERENCES "t_tape" ("id") ON DELETE CASCADE;