# CLAUDE.md

This file provides guidance to Claude Code when working in `internal/`.

## 领域模型

```
dataset SQLite ──dataset_uid──> PG t_dataset_ref ──r_arcset_dataset_ref──> arcset
       └── DATA shard ──> segment ──> file
```

| 包 | 存储 | 说明 |
|----|--------|------|
| `dataset` | dataset SQLite：`t_dataset`、`t_file`、`t_shard`(DATA)、`t_segment` | 不可变文件集合，`CreateFromDir` 扫描目录创建 |
| `datasetops` | dataset SQLite | CLI/API 共享的 create、mark-built、make-shards、scan |
| `arcset` | PG：`t_arcset`、`t_dataset_ref`、`r_arcset_dataset_ref`、`t_shard` | 多 dataset 的聚合容器，`GenerateSegments` 切分为 `SegmentDesc` |
| `arcsetops` | PG + dataset SQLite | CLI/API 共享的 create、append、expand |
| `shard` | dataset SQLite + PG | dataset 层构建 DATA shard；arcset 层管理 DATA/EC/PAD 布局 |
| `builddata` | dataset SQLite + PG | 排序 shard、placement、PAD 复用、移动文件 |
| `makeec` | PG + 文件 | 按 stripe 生成 EC/PAD |
| `recoverec` | PG + 文件 | 流式恢复 DATA/EC |
| `validate` | PG + 文件 | data/ec/all 校验 |
| `export` | PG | 生成 arcset.json |
| `api` | dataset SQLite + PG | REST handler；不直接操作旧全局 SQLite |

依赖方向：`shard` → `arcset`（使用 `SegmentDesc`），`arcset` 直连 `t_file` 查询。

当前实现：`dataset` 包有 `DatasetStore` 操作每个 dataset 的 SQLite；arcset 层（`t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard`、`t_fs_mount`）走 `arcset.PGStore`。CLI 与 API 共享 `datasetops` / `arcsetops`，避免重复业务逻辑。无跨库外键，用 `dataset_uid` + `seq` 逻辑关联。旧的 `SQLiteStore` / `OpenSQLite` 只保留给 `shard validate` 的未迁移 bin 路径。

## 包约定

```
internal/<package>/
├── <package>.go      # 领域模型 struct
├── store.go          # Store 接口
├── store_sqlite.go   # SQLiteStore（*sql.DB 注入）
├── store_pg.go       # PGStore（arcset 层）
├── create.go         # 创建业务逻辑
├── list.go           # 列表查询
```

- Store 接口定义持久化操作，SQLiteStore 用 `*sql.DB` 注入（`sql.Open("sqlite3", ":memory:")` 做测试）。
- Create 方法通过 `result.LastInsertId()` 回写对象 ID。
- nullable 字段 scan 时用 `sql.NullString` / `sql.NullTime` / `sql.NullInt64`。
- 批量写入用 `BatchAddFileRecords`（多值 INSERT，缺省 199 行/批），比逐条 INSERT 快 10x 以上。环境变量 `PACKFS_BATCH_INSERT` 可调整批大小。

## 技术栈

- 错误处理：`github.com/kaichao/gopkg/errors`（`errors.E` / `errors.WrapE` / `errors.NewUsage`）
- 日志：`github.com/sirupsen/logrus`
- SQLite：`github.com/mattn/go-sqlite3`，占位符用 `?`
- 压缩：`github.com/klauspost/compress/zstd`、`github.com/ulikunitz/xz`
- schema 嵌入：`internal/db/dataset_schema.sql`、`internal/db/postgres_schema.sql`

## 数据库

- dataset：每个 dataset 一个 SQLite，入口 `internal/db.OpenDatasetSQLite(path, readOnly)`；新文件自动应用 `dataset_schema.sql`。
- arcset：全局 PostgreSQL，入口 `internal/db.OpenPostgres(ctx)` / `PACKFS_PG_URL`；`arcset.PGStore`。
- schema 源文件：`build/sqlite/dataset_schema.sql`、`build/postgres/arcset_schema.sql`；`internal/db/*_schema.sql` 为嵌入式副本，修改时必须同步。
- 旧全局 `SQLITE_DB` / `OpenSQLite` 只供未迁移的旧命令使用，新代码不要依赖。
