# CLAUDE.md

This file provides guidance to Claude Code when working in `internal/`.

## 领域模型

```
dataset SQLite ──dataset_uid──> PG t_dataset_ref ──r_arcset_dataset_ref──> arcset
       └── DATA shard ──> segment ──> file
```

| 包 | 对应表（目标） | 说明 |
|----|--------|------|
| `dataset` | dataset SQLite：`t_dataset`、`t_file`、`t_shard`(DATA)、`t_segment` | 不可变文件集合，`CreateFromDir` 扫描目录创建 |
| `arcset` | PG：`t_arcset`、`t_dataset_ref`、`r_arcset_dataset_ref`、`t_shard` | 多 dataset 的聚合容器，`GenerateSegments` 切分为 `SegmentDesc` |
| `shard` | dataset SQLite + PG | dataset 层构建 DATA shard；arcset 层管理 DATA/EC/PAD 布局 |

依赖方向：`shard` → `arcset`（使用 `SegmentDesc`），`arcset` 直连 `t_file` 查询。

目标架构（尚未实现）：`dataset` 包只操作每个 dataset 的 SQLite；arcset 层（`t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard`）走 PostgreSQL Store。无跨库外键，用 `dataset_uid` + `seq` 逻辑关联。

## 包约定

```
internal/<package>/
├── <package>.go      # 领域模型 struct
├── store.go          # Store 接口
├── store_sqlite.go   # SQLiteStore（*sql.DB 注入）
├── store_pg.go       # 目标：PGStore（arcset 层，尚未实现）
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
- schema 嵌入：`internal/db/schema.sql` 通过 `//go:embed` 嵌入

## 数据库

- SQLite，路径由 `SQLITE_DB` 环境变量指定，缺省 `data/packfs.db`
- 首次创建时自动用嵌入的 `schema.sql` 建表
- 入口：`internal/db.OpenSQLite()`
- 目标架构（尚未实现）：每个 dataset 一个 SQLite，arcset 元数据在 PostgreSQL；见 `docs/design/storage-architecture.md`
