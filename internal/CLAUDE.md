# CLAUDE.md

This file provides guidance to Claude Code when working in `internal/`.

## 领域模型

```
dataset ──(r_arcset_dataset)──> arcset ──> shard ──> segment ──> file
```

| 包 | 对应表 | 说明 |
|----|--------|------|
| `dataset` | `t_dataset`、`t_file` | 不可变文件集合，`CreateFromDir` 扫描目录创建 |
| `arcset` | `t_arcset`、`r_arcset_dataset` | 多 dataset 的聚合容器，`GenerateSegments` 切分为 `SegmentDesc` |
| `shard` | `t_shard`、`t_segment` | 打包大文件，`CreateShard` 读取源文件写入物理 shard |

依赖方向：`shard` → `arcset`（使用 `SegmentDesc`），`arcset` 直连 `t_file` 查询。

## 包约定

```
internal/<package>/
├── <package>.go      # 领域模型 struct
├── store.go          # Store 接口
├── store_sqlite.go   # SQLiteStore（*sql.DB 注入）
├── create.go         # 创建业务逻辑
├── list.go           # 列表查询
```

- Store 接口定义持久化操作，SQLiteStore 用 `*sql.DB` 注入（`sql.Open("sqlite3", ":memory:")` 做测试）。
- Create 方法通过 `result.LastInsertId()` 回写对象 ID。
- nullable 字段 scan 时用 `sql.NullString` / `sql.NullTime` / `sql.NullInt64`。

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
