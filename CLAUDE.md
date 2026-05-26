# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建与测试

```sh
go build ./...
go test ./internal/...          # 全部测试
go test -v ./internal/dataset/  # 单包测试
```

没有 Makefile（除 build/postgres/Makefile 用于打包 Docker 镜像外）。

## 架构

packfs 是一个文件打包系统，将目录中的小文件合并成大文件（shard），以减少磁盘碎片。核心领域模型：

```
dataset ──(r_arcset_dataset)──> arcset ──> shard ──> segment ──> file
```

- **dataset**：不可变的文件集合，从磁盘目录扫描创建（`CreateFromDir`）。记录在 t_dataset + t_file。
- **arcset**：归档集，是多个 dataset 的聚合容器，也是 shard 的父级。创建时关联 dataset，可用 `GenerateSegments` 按 `segment_bytes` 将关联的所有文件切分为 `SegmentDesc` 列表。
- **shard**：打包后的大文件，由多个 segment 组成。`CreateShard` 消费 `[]arcset.SegmentDesc`，读取源文件写入 shard 物理文件，计算 checksum，写入 t_shard + t_segment。
- **segment**：shard 内的一个片段，对应源文件的某个字节范围。

依赖方向：`shard` → `arcset`（使用 `SegmentDesc`），`arcset` 直连 t_file 查询（不依赖 dataset Store）。

## 数据库

- 主要使用 SQLite，数据库路径由环境变量 `SQLITE_DB` 指定。不存在时用 `build/sqlite/erd.sql` 初始化。
- 支持 PostgreSQL 作为扩展，schema 在 `build/postgres/initdb.d/20-erd.sql`。
- ER 图见 `packfs-erd.png`。

## 代码模式

每个 internal 包遵循一致的约定：

```
internal/<package>/
├── <package>.go      # 领域模型 struct（Dataset, File, Filter, Update 等）
├── store.go          # Store 接口定义
├── store_sqlite.go   # SQLiteStore 实现（*sql.DB 注入）
├── create.go         # 创建业务逻辑
├── list.go           # 列表查询封装（薄层包装 Store 方法）
```

- Store 接口定义持久化操作，SQLiteStore 用 `*sql.DB` 注入（支持 `:memory:` 做测试）。
- 错误处理用 `github.com/kaichao/gopkg/errors`（`errors.E` / `errors.WrapE`），日志用 `github.com/sirupsen/logrus`。
- SQLite SQL 用 `?` 占位符，不是 `$1`。
- Create 方法通过 `result.LastInsertId()` 回写对象的 ID 字段。

## CLI

`cmd/cli/main.go` 是 cobra 命令行入口，命令包括 `make-arcset`、`make-shard`、`mount`、`umount`、`fsck` 等。大部分命令还是骨架实现。
