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

packfs 是一个文件打包系统，将目录中的小文件合并成大文件（shard），减少磁盘碎片。支持 EC 纠删码（Reed-Solomon），允许丢失 ≤m 个 shard 仍可恢复。

```
dataset ──(r_arcset_dataset)──> arcset ──> shard ──> segment ──> file
```

- **dataset**：不可变文件集合，从磁盘目录扫描创建，持有存储配置（format/compress/shard_max_bytes）
- **arcset**：EC 纠删码容器，多个 dataset 通过 append 关联
- **shard**：打包大文件，由多个 segment 组成，含 SHA-256 checksum。支持 bin / tar / iso 三种格式，DATA / EC / PAD 三种类型
- **segment**：shard 内一个片段，对应源文件的某个字节范围

## 目录结构

| 目录 | 用途 | 详见 |
|------|------|------|
| `internal/` | 领域模型 + Store + 业务逻辑 | `internal/CLAUDE.md` |
| `cmd/cli/` | CLI 适配层 + cobra 命令 | `cmd/cli/README.md`、`cmd/cli/CLAUDE.md` |
| `docs/` | 设计文档 + TODO | `docs/design.md`、`docs/todo.md` |
| `webui/` | 纯前端管理面板（模拟模式） | `webui/README.md` |
| `build/` | SQL schema（pg/sqlite）、Docker | — |

## 数据库

SQLite 为主，路径由 `SQLITE_DB` 环境变量指定（缺省 `~/data/packfs.db`），首次自动建表。支持 PostgreSQL 扩展。

大部分业务字段（create_time, format, shard_max_bytes 等）存在 JSON `metadata` 列中，表结构仅保留 id、name、关联 FK 等核心列。

`dataset create` 用 `BatchAddFileRecords`（多值 INSERT，199 行/批），10 万文件 ~11s。`PACKFS_BATCH_INSERT` 可调批大小。

## 状态机

```
dataset:   active ──> archived        (dataset finalize 后)
              └─────> absorbed        (make-ec 后，数据已写入 EC shard)

arcset:    building ──> complete ──> ready ──> taped
              (validate 全过)  (finalize)  (写入磁带)
```

- `active`：文件在 `current_path`，可直接访问
- `archived`：数据已打包到 shard
- `absorbed`：数据已通过 EC 编码写入 arcset，不再作为独立的 data shard 管理
- `building`：dataset 已关联，shard 正在生成 / EC 可在此阶段执行
- `complete`：所有 shard 校验通过，数量 ≥ `metadata["shard_count"]`
- `ready`：已 finalize，DB 已复制，目录自包含
- `taped`：已写入磁带

## EC 纠删码

`shard make-ec` 将 arcset 中的 data shard 按 RS(k+m) 编码。参数约束：k+m ≤ 255，k ≤ 24，m ∈ {2,4,6}。

```sh
# 创建 arcset 时指定 EC 参数
packfs arcset create --name=my-arc --target-root=/data/arc --ec=8+4

# 关联 dataset 后执行 EC 编码
packfs arcset append --id=1 --dataset-id=1
packfs shard make-ec --arcset-id=1

# 恢复丢失的 shard
packfs shard recover --arcset-id=1 --shard-file=1D1_0000.tar

# 修改 EC 参数后重新编码
packfs arcset rebuild --id=1 --ec=12+4
```

EC 编码后的文件命名：`<stripe>D<position>_<原名>`（数据）/ `<stripe>E<position>.<ext>`（校验）。

## Dataset Finalize

```sh
packfs dataset finalize --id=1
```

1. 校验所有 shard checksum
2. 确认 shard 数量 ≥ `shard_count`
3. 复制 `SQLITE_DB` → `current_path/packfs.db`
4. dataset → `archived`

## Shard 分组逻辑

1. 按 **dataset 为粒度** 分组文件（一个 shard 不跨 dataset）。
2. 组内文件按顺序累加，超过 `shard_max_bytes` 时关闭当前 shard，新建下一个。
3. 单个文件超过 `shard_max_bytes` 时拆分多段，不超时文件保持完整。
4. shard 序号从 0 开始，4 位整数命名（如 `0000.tar`）。格式由 dataset metadata 控制。

EC 后的 shard 文件命名：
- Data: `<stripe>D<position>_<原名>`（如 `1D1_0000.tar`）
- EC:   `<stripe>E<position>.<ext>`（如 `1E3.tar`）
- PAD:  `<stripe>D<position>_pad.<ext>`（如 `1D3_pad.tar`）

## Shard 定义文件（.def）

```
# dataset_id: 3
相对路径/a.txt
相对路径/b.txt
```

- `# dataset_id` 由 gen-def / 外部脚本写入，`shard make` 据此从 DB 读取配置。
- `# arcset_id` 可选，arcset 模式下 `shard make` 从 arcset metadata 读取 format/compress。
- `shard make` 支持 `--output-dir` 覆盖输出目录。
- 压缩模式和打包格式从 dataset（或 arcset）metadata 读取，def 文件名中的扩展名仅用于标识。
