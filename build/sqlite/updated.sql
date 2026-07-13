-- ============================================================
-- packfs 重构：erd.sql → refactoring-plan.md Section 1.1
-- 增量修改脚本，应用于 ER 图工具生成的 erd.sql 之后
-- SQLite 3.37.2 不支持 ALTER TABLE ADD CHECK / ALTER COLUMN，
-- 因此 T_shard + T_segment 通过 DROP + CREATE 重建
-- ============================================================

-- 1. 重建 T_shard
--    变更：arcset/dataset FK 改为 nullable + CHECK 约束
--          ON DELETE SET NULL → ON DELETE CASCADE（避免 CHECK 冲突）
--          file_path 去掉 DEFAULT VARCHAR

DROP TABLE IF EXISTS "T_shard";

CREATE TABLE "T_shard" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "seq" INTEGER,
  "file_path" TEXT,
  "file_size" BIGINT,
  "type" VARCHAR,
  "metadata" JSON,
  "sha256" VARCHAR,
  "last_check" DATETIME,
  "arcset" INTEGER REFERENCES "T_arcset" ("id") ON DELETE CASCADE,
  "dataset" INTEGER REFERENCES "T_dataset" ("id") ON DELETE CASCADE,
  CHECK ("dataset" IS NOT NULL OR "arcset" IS NOT NULL)
);

-- 2. 条件唯一索引（替换 erd.sql 中的普通索引）

CREATE INDEX "idx_t_shard__arcset" ON "T_shard" ("arcset");
CREATE INDEX "idx_t_shard__dataset" ON "T_shard" ("dataset");

CREATE UNIQUE INDEX "idx_t_shard__dataset_file_path"
  ON "T_shard" ("dataset", "file_path") WHERE "dataset" IS NOT NULL;

CREATE UNIQUE INDEX "idx_t_shard__arcset_file_path"
  ON "T_shard" ("arcset", "file_path") WHERE "arcset" IS NOT NULL;

-- 3. 重建 T_segment（DROP T_shard 时被级联删除，结构不变）

DROP TABLE IF EXISTS "T_segment";

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
