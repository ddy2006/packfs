# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建与测试

```sh
go build ./...
go test ./internal/...          # 全部测试
go test -v ./internal/dataset/  # 单包测试
```

没有 Makefile（除 `build/postgres/Makefile` 用于 Docker 镜像外）。

## 项目概述

packfs 是一个文件打包系统，将目录中的小文件合并成大文件（shard），减少磁盘碎片。

```
dataset ──(r_arcset_dataset)──> arcset ──> shard ──> segment ──> file
```

- **dataset**：不可变文件集合，从磁盘目录扫描创建
- **arcset**：多个 dataset 的聚合容器，shard 的父级
- **shard**：打包大文件，由多个 segment 组成，含 SHA-256 checksum。支持 bin 和 tar 两种格式
- **segment**：shard 内一个片段，对应源文件的某个字节范围

## 目录结构

| 目录 | 用途 | 详见 |
|------|------|------|
| `internal/` | 领域模型 + Store + 业务逻辑 | `internal/CLAUDE.md` |
| `cmd/cli/` | CLI 适配层 + cobra 命令 | `cmd/cli/README.md`、`cmd/cli/CLAUDE.md` |
| `build/` | SQL schema（pg/sqlite）、Docker | — |

## 数据库

SQLite 为主，路径由 `SQLITE_DB` 环境变量指定（缺省 `~/data/packfs.db`），首次自动建表。支持 PostgreSQL 扩展。

大部分业务字段（create_time, format, shard_max_bytes 等）存在 JSON `metadata` 列中，表结构仅保留 id、name、关联 FK 等核心列。

## Shard 分组逻辑

1. `gen-def` 按 **dataset 为粒度** 分组文件（一个 shard 不跨 dataset）。
2. 组内文件按顺序累加，超过 `shard_max_bytes` 时关闭当前 shard，新建下一个。
3. 单个文件不拆分，即使超过 `shard_max_bytes` 也独占一个 shard。
4. shard 序号从 0 开始，4 位整数命名。格式由 `metadata["format"]` 和 `metadata["compress"]` 决定：
   - bin, 无压缩: `0000.bin.def`
   - bin, shard 级 zstd: `0000.bin.zst.def`
   - tar, 无压缩: `0000.tar.def`
   - tar, shard 级 zstd: `0000.tar.zst.def`

## Shard 定义文件（.def）

```
# arcset_id: 1
# dataset_id: 3
相对路径/a.txt
相对路径/b.txt
```

- `# arcset_id` 和 `# dataset_id` 由 `gen-def` 自动写入。
- `shard make` 据此从 DB 获取输出目录（`t_arcset.current_path`）和源目录（`t_dataset.current_path`），无需手动传 `--source-root` / `--target-root`。
- `shard make` 打包时校验文件大小与 DB 记录是否一致，不一致输出 warning。
- 压缩模式和打包格式从 arcset 的 `metadata["compress"]` / `metadata["format"]` 读取，def 文件名中的扩展名仅用于标识。
