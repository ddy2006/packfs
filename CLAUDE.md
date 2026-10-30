# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 构建与测试

```sh
go build ./...                  # 全量编译
go build -o packfs ./cmd/cli    # 编译 CLI 二进制
go test ./internal/...          # 全部测试
go test -v ./internal/dataset/  # 单包测试
PACKFS_TEST_PG_URL=postgres://... go test ./internal/...  # 含 PG 集成测试
```

PG 集成测试使用临时 schema（`internal/pgtest`），未设置 `PACKFS_TEST_PG_URL` 时自动跳过；不会污染 public schema。

没有 Makefile（除 `build/postgres/Makefile` 用于 Docker 镜像外）。

## WebUI

```sh
packfs serve --addr=:8080 --db-dir=/data/packfs/datasets  # 启动管理面板
# 浏览器打开 http://localhost:8080
```

- REST API 在 `internal/api/`（`handler.go` 路由/helpers，`dataset.go`、`arcset.go`、`shard.go`、`fs.go` 等端点）；dataset 端点读 `--db-dir` 下的 SQLite，arcset 端点读 PG。
- PostgreSQL 通过 `PACKFS_PG_URL` 配置；未配置时 dataset 功能可用，arcset 端点返回 503。
- 前端在 `webui/`，编译时通过 `embed.go` 的 `//go:embed all:webui` 嵌入。
- `cmd/cli/webui/serve.go`：serve 命令 + 路由注册；`--db-dir` 缺省 `$PACKFS_DATASET_DB_DIR` 或 `~/data/packfs/datasets`。
- 工作流管道末端为“移交下游存储”，不展示介质/磁带状态。

## 项目概述

packfs 是一个文件打包系统，将目录中的小文件合并成大文件（shard），减少磁盘碎片。支持 EC 纠删码（Reed-Solomon），允许丢失 ≤m 个 shard 仍可恢复。

```
dataset SQLite ──dataset_uid──> PG t_dataset_ref ──r_arcset_dataset_ref──> arcset
       └── DATA shard ──> segment ──> file
```

- **dataset**：不可变文件集合；每个 dataset 一个 SQLite，持有 file/segment/DATA shard 与存储配置（format/compress/shard_max_bytes）
- **arcset**：EC 纠删码容器，元数据在全局 PostgreSQL；多个 dataset 通过 append 关联
- **shard**：打包大文件，由多个 segment 组成，含 SHA-256 checksum。dataset 层只有 DATA；arcset 层有 DATA / EC / PAD
- **segment**：shard 内一个片段，对应源文件的某个字节范围

## 目录结构

| 目录 | 用途 | 详见 |
|------|------|------|
| `internal/` | 领域模型 + Store + 业务逻辑 | `internal/CLAUDE.md` |
| `cmd/cli/` | CLI 适配层 + cobra 命令 | `cmd/cli/README.md`、`cmd/cli/CLAUDE.md` |
| `docs/` | 设计文档 + TODO | `docs/design.md`、`docs/design/`、`docs/todo.md` |
| `webui/` | 前端管理面板（真实 API 驱动） | `webui/README.md` |
| `internal/api/` | REST handler（dataset SQLite + PG） | 本文件 |
| `internal/fuse/` | 只读 FUSE 文件系统（hanwen/go-fuse/v2） | 本文件 |
| `internal/simulate/` | 仿真数据生成（crypto/rand） | 本文件 |
| `cmd/cli/webui/` | `packfs serve` 命令 | 本文件 |
| `cmd/cli/fs/` | `packfs fs mount` 命令 | 本文件 |
| `build/` | SQL schema（pg/sqlite）、Docker | — |

## 数据库

核心链路使用两层存储，见 `docs/design/storage-architecture.md`：

- 每个 dataset 一个 SQLite：`--db-dir` / `PACKFS_DATASET_DB_DIR`，缺省 `~/data/packfs/datasets/<uid>.db`；保存 `t_dataset` / `t_file` / `t_shard`(DATA) / `t_segment`。
- arcset 元数据使用全局 PostgreSQL：`PACKFS_PG_URL`；保存 `t_dataset_ref` / `t_arcset` / `r_arcset_dataset_ref` / `t_shard`(DATA/EC/PAD) / `t_fs_mount`。
- 旧 `SQLITE_DB` / `~/data/packfs.db` 仅剩旧命令路径，逐步清理；新代码不要依赖它。
- 大部分业务字段在 JSON `metadata` 列中，表结构只保留 id、关联键和核心状态。

`dataset create` 用 `BatchAddFileRecords`（多值 INSERT，199 行/批），10 万文件 ~11s。`PACKFS_BATCH_INSERT` 可调批大小。

## 状态机

```
dataset SQLite:  building ──所有 DATA shard 完成──> built ──首次 arcset 注册──> frozen
arcset:          defined ──> building ──> completed
                                ▲             │
                                └── append ───┘
```

- `building` / `built`：dataset SQLite 状态；build 完成 = 所有 DATA shard 生成并写入 checksum。
- `frozen`：首次 `arcset append` 在 PG 注册后只读；冻结后只能新建 dataset。
- 不记录 relation 的 registered/absorbed 状态；DATA shard 数量表示 build-data 是否完成。
- `defined` / `building` / `completed`：arcset 状态；`build_phase` / `health` / `parity_degraded` 见 `docs/design/arcset-lifecycle.md`。
- 介质写入状态属于下游存储系统，packfs 不记录。

## EC 纠删码

`arcset make-ec` 将 arcset 中的 DATA shard 按 RS(k+m) 编码。参数约束：k 1..24；m 为 0 或 `{2,4,6}`；`k+m ≤ 24`。`m=0` 表示不启用 EC。

100GB 级 shard 使用 `internal/ec/stream.go` 的分块流式编码/恢复/校验；`internal/makeec`、`internal/recoverec`、`internal/validate` 已使用。旧 `internal/ec` 非流式 API 仅供测试兼容。

```sh
# 创建 arcset 时指定 EC 参数（--ec=8 表示无 EC）
packfs arcset create --name=my-arc --target-root=/data/arc --ec=8+4

# 注册 dataset（dataset SQLite）后执行 EC 编码
packfs arcset append --id=1 --dataset-db=/data/datasets/<uid>.db
packfs arcset build-data --id=1
packfs arcset make-ec --id=1

# 恢复丢失的 shard
packfs arcset recover --id=1 --shard-file=00/000D-ds3f2a1c9b-0000.tar
```

EC 后的文件命名：`<partition>/<seq>D-<dataset_key>-<原名>`（数据）/ `<partition>/<seq>E.<ext>`（校验）/ `<partition>/<seq>P.<ext>`（PAD）。

## dataset build 完成

1. 校验所有 DATA shard checksum。
2. 确认 shard 数量 ≥ `shard_count`。
3. `t_dataset.status=built`；dataset SQLite 保持可写，首次 arcset 注册后冻结。
4. `--gen-only` + 外部 `shard make` 完成后执行 `dataset mark-built`；不再使用 `dataset finalize`，不复制全局 `packfs.db`。

## Shard 分组逻辑

1. 按 **dataset 为粒度** 分组文件（一个 shard 不跨 dataset）。
2. 组内文件按顺序累加，超过 `shard_max_bytes` 时关闭当前 shard，新建下一个。
3. 单个文件超过 `shard_max_bytes` 时拆分多段，不超时文件保持完整。
4. shard 序号从 0 开始，4 位整数命名（如 `0000.tar`）。格式由 dataset metadata 控制。

EC 后的 shard 文件命名（目标）：
- Data: `<partition>/<seq>D-<dataset_key>-<原名>`（如 `00/000D-ds3f2a1c9b-0000.tar`）
- EC:   `<partition>/<seq>E.<ext>`（如 `08/000E.tar`）
- PAD:  `<partition>/<seq>P.<ext>`（如 `05/000P.tar`）

## FUSE 挂载

```sh
packfs fs mount --dataset-db=/data/datasets/<uid>.db --mount-point=/mnt/pk  # 仅未吸收 dataset
packfs fs mount --arcset-id=1 --mount-point=/mnt/pk                          # arcset 挂载
fusermount -u /mnt/pk
```

- 基于 `hanwen/go-fuse/v2`，只读；bin 随机读最优
- dataset 挂载直接读 dataset SQLite；吸收后必须用 arcset 挂载
- arcset 挂载读 PG + 各 dataset SQLite；根目录为 `<dataset_key>/`
- 无压缩直接 seek+read；segment 级压缩按需解压；shard 级压缩全量解压（慢）
- 物理 shard 缺失时读取返回 `ENOENT`

## Shard 定义文件（.def）

```
# dataset_db: /data/datasets/<uid>.db
相对路径/a.txt
相对路径/b.txt
```

- `# dataset_db` 由 gen-def / 外部脚本写入，`shard make` 据此打开 dataset SQLite。
- `# arcset_id` 可选，arcset 模式下 `shard make` 从 PG / arcset metadata 读取 format/compress。
- `shard make` 支持 `--output-dir` 覆盖输出目录。
- 压缩模式和打包格式从 dataset（或 arcset）metadata 读取，def 文件名中的扩展名仅用于标识。
