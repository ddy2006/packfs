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
dataset ──> arcset ──> shard ──> segment ──> file
```

- **dataset**：不可变文件集合，从磁盘目录扫描创建
- **arcset**：多个 dataset 的聚合容器，shard 的父级
- **shard**：打包大文件，由多个 segment 组成，含 SHA-256 checksum
- **segment**：shard 内一个片段，对应源文件的某个字节范围

## 目录结构

| 目录 | 用途 | 详见 |
|------|------|------|
| `internal/` | 领域模型 + Store + 业务逻辑 | `internal/CLAUDE.md` |
| `cmd/cli/` | CLI 适配层 + cobra 命令 | `cmd/cli/README.md`、`cmd/cli/CLAUDE.md` |
| `build/` | SQL schema（pg/sqlite）、Docker | — |

## 数据库

SQLite 为主，路径由 `SQLITE_DB` 环境变量指定（缺省 `data/packfs.db`），首次自动建表。支持 PostgreSQL 扩展。
