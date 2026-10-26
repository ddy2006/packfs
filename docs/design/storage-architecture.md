# 两层数据库架构

> 目标：dataset 元数据本地化、不可变；arcset 元数据全局化、可查询。两层之间不做外键，只做逻辑引用。
> 本文是 [commands-db.md](commands-db.md)、[arcset-lifecycle.md](arcset-lifecycle.md)、[fuse.md](fuse.md) 的存储基础；EC 大文件处理见 [ec-streaming.md](ec-streaming.md)。

## 1. 分层

| 层 | 存储 | 内容 | 生命周期 |
|----|------|------|----------|
| dataset 层 | 每个 dataset 一个 SQLite 文件 | `t_dataset`、`t_file`、`t_segment`、`t_shard`（仅 DATA） | 本地创建；build 完成后首次注册到 arcset 即冻结；只读 |
| arcset 层 | 全局 PostgreSQL | `t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard`（DATA/EC/PAD） | 随 arcset 构建、校验、恢复、扩容变化 |
| 挂载状态 | 全局 PostgreSQL | `t_fs_mount` | 挂载记录统一在 PG；dataset SQLite 只读 |

- dataset SQLite 是 file → segment → DATA shard 的权威来源。
- PG 是 arcset 布局、DATA/EC/PAD 清单、容量与健康状态的权威来源。
- 两边都有 `t_shard`，但语义不同，代码中使用不同模型：`dataset.Shard` / `arcset.Shard`。
- 没有跨库外键；逻辑键是 `dataset_uid` 和 `seq`。
- 一个 dataset 只加入一个 arcset；需要第二份归档时重新创建 dataset。packfs 不维护多 arcset 复用关系。

连接与路径：

- `PACKFS_PG_URL`：PostgreSQL DSN。
- dataset SQLite：`--db-dir` 指定；缺省 `$PACKFS_DATASET_DB_DIR`，未设置时 `~/data/packfs/datasets`。
- 挂载状态：PG `t_fs_mount`；dataset SQLite 保持只读。

## 2. 全局身份

- `dataset_uid`：本地生成 UUID；写入 dataset SQLite 的 `t_dataset.uid`，PG 的 `t_dataset_ref.uid` 与之对应。
- SQLite 文件名：`<dataset_uid>.db`；路径由 PG 的 `t_dataset_ref.sqlite_path` 记录。
- `dataset_key`：缺省 `ds<UID 去连字符后的前 8 位>`，例如 `ds3f2a1c9b`；显式 `--dataset-key` 优先；冲突直接报错。
- SQLite 内的 `t_dataset.id`、`t_shard.id` 只在本地有效，不用于跨库引用。
- PG 的 DATA shard 逻辑键：`(arcset, dataset_ref, seq)`；`seq` 来自 dataset SQLite。

## 3. 生命周期

```text
dataset SQLite:  building ──所有 DATA shard 完成──> built
                                                    │ 首次 arcset 注册
                                                    ▼
                                                frozen（只读）
```

- build 完成 = 所有 DATA shard 已生成、checksum 已写入，`t_dataset.status='built'`；`--gen-only` 在 `shard make` 完成全部 shard 后置 `built`。
- 首次 `arcset append` 在 PG 注册成功后，dataset SQLite 冻结；冻结后只能新建 dataset。
- 不记录 relation 的 registered/absorbed 状态；`build-data` 是否完成由 PG 中该 dataset 的 DATA shard 数量判断。
- recover 重建的是原始 DATA shard 的字节内容；只要源 file 不变，SHA-256 不变。recover 只更新 PG 的 `last_check` 和 arcset `health`。
- 下游介质状态（`on_media`）不在 packfs 记录。

### 3.1 冻结与注册的非原子性

`arcset append` 有两个动作，不在同一事务：

1. 在 PG 写 `t_dataset_ref` / `r_arcset_dataset_ref`；
2. 冻结 dataset SQLite（只读权限 / 只读连接）。

失败模式：

| 场景 | 结果 | 处理 |
|------|------|------|
| PG 成功，冻结失败 | relation 已存在，SQLite 仍可写 | `append` 重试时检测 relation，补做冻结；router 串行，不会继续 build-data |
| 冻结成功，PG 失败 | SQLite 只读，无 relation | `append` 重试补写 relation；若放弃，人工恢复权限或新建 dataset |
| 两者成功但响应丢失 | 幂等重试返回已有 key | 不重复插入 |

结论：在“append 幂等 + 控制面串行 + 一 dataset 一 arcset”的前提下，非原子不会造成数据损坏，只会产生可修复的中间状态。


## 4. dataset SQLite schema

完整 DDL 见 [`build/sqlite/dataset_schema.sql`](../../build/sqlite/dataset_schema.sql)。

| 表 | 关键字段 | 说明 |
|----|----------|------|
| `t_dataset` | `id`、`uid`、`name`、`status`、`metadata`、`current_path` | `id` 仅本地；`uid` 全局唯一；`status` 为 `building` / `built` |
| `t_file` | `id`、`file_path`、`file_size`、`metadata`、`sha256`、`dataset` | 原始文件清单 |
| `t_shard` | `id`、`seq`、`file_path`、`file_size`、`type=DATA`、`sha256` | 仅 DATA；`file_path` 为 dataset 内原名 |
| `t_segment` | `offset`、`size`、`csize`、`shard`、`file` | shard 内片段 |

- 没有 arcset / partition / group_no / stripe_seq / pos 字段。
- `t_dataset.metadata.shard_root` 记录 DATA shard 所在目录；未设置时用 `current_path`。
- 冻结通过文件权限与只读连接实现；`status` 保持 `built`，不新增 frozen 状态。
- 冻结后不再写入 dataset SQLite。

## 5. PostgreSQL schema

完整 DDL 见 [`build/postgres/arcset_schema.sql`](../../build/postgres/arcset_schema.sql)。

| 表 | 关键字段 | 说明 |
|----|----------|------|
| `t_dataset_ref` | `id`、`uid`、`name`、`sqlite_path` | 全局 dataset 身份；`id` 为 PG 主键 |
| `t_arcset` | `id`、`name`、`metadata`、`status`、`build_phase`、`health`、`parity_degraded` | arcset 参数与状态 |
| `r_arcset_dataset_ref` | `arcset`、`dataset_ref`、`dataset_key`、`append_order` | `UNIQUE(dataset_ref)` 保证一 dataset 一 arcset |
| `t_shard` | `arcset`、`dataset_ref`、`dataset_key`、`seq`、`stripe_seq`、`group_no`、`pos`、`partition`、`file_path`、`type` | DATA/EC/PAD 布局与快照 |
| `t_fs_mount` | `mount_point`、`type`、`dataset_uid`、`arcset` | 挂载记录统一在 PG；dataset 用 uid 逻辑引用 |

索引与约束：

- `UNIQUE(arcset, file_path)`
- `UNIQUE(arcset, group_no, stripe_seq, pos)`
- `UNIQUE(arcset, partition, stripe_seq)`
- `UNIQUE(arcset, dataset_ref, seq) WHERE type='DATA'`
- `UNIQUE(dataset_ref)`、`UNIQUE(arcset, dataset_key)`、`UNIQUE(arcset, append_order)`（关系表）

说明：

- PG 不保存 `t_file` / `t_segment`。
- DATA shard 的 `file_size` / `sha256` 是从 dataset SQLite 复制的快照，供 manifest、容量汇总和下游校验。
- `missing` / `corrupt` 由 validate 结果和 arcset `health` 表达。
- `used_capacity` 按已写入的 `t_shard.file_size` 聚合。
- EC/PAD 的 `dataset_ref` 为空；`seq` 仍表示同条带的数据序号。

## 6. 跨库操作顺序

### append

1. 读取 dataset SQLite，确认 `status=built`、所有 DATA shard 已生成。
2. 在 PG 写 `t_dataset_ref`（若首次）与 `r_arcset_dataset_ref`；分配 `dataset_key` / `append_order`。
3. 冻结 dataset SQLite（只读权限 / 只读连接）。
4. 不移动文件；build-data 是否完成由 PG 中该 dataset 的 DATA shard 数量判断。重试时若 relation 已存在，补做冻结并返回已有 key。

### build-data

1. 读 dataset SQLite 的 DATA shard 清单。
2. 计算完整 placement；容量按已写入的 `t_shard.file_size` 聚合。
3. 移动 shard 文件到 partition 目录。
4. 写入 PG `t_shard` 行（DATA）；全部完成后不再需要额外 relation 状态。
5. 崩溃重试：目标路径确定；已存在的目标文件按 checksum 校验，已存在的行复用，缺失的行补写。

### make-ec / validate / recover

- 大文件必须使用 [ec-streaming.md](ec-streaming.md) 的分块流式 API；禁止整 shard 读入内存。
- 只读写 PG 与 arcset 工作目录；不修改 dataset SQLite。
- recover 重建 DATA shard 后校验 SHA-256；必须与 PG 记录一致，不一致视为失败。
- recover 成功只更新 PG `last_check` 和 arcset `health`。

### export

- 从 PG 生成 `arcset.json`；不复制 dataset SQLite。
- 不再生成 arcset 范围 `packfs.db`。

## 7. 一致性、并发与文件移动

- 没有跨库事务；PG `t_shard` 行是 placement 的记录。
- `used_capacity` 按已写入的 `t_shard.file_size` 聚合。
- **控制面串行**：同一 arcset 的 `append`、`build-data`、`make-ec`、`expand`、`recover`、`export` 由 scalebox router 串行调用。packfs 不内置 arcset 锁；绕过 router 并发调用同一 arcset 的行为未定义。router 实现与故障恢复由 scalebox 项目负责，不在 packfs 范围。
- **数据面并行**：不同 stripe 的 EC 编码可以并行；同一 stripe 的操作必须幂等。并行度受磁盘带宽和 chunk 内存上限约束。
- **跨节点路径**：PG 的 `sqlite_path` 必须对 router/执行节点可见；共享存储或本地副本由 scalebox 负责，packfs 不做跨节点同步。FUSE/export 同样要求执行节点能访问该路径。
- 同一文件系统优先 `rename`；跨文件系统按“复制 + 校验 + rename + 删除源文件”处理。
- 重试必须幂等：目标路径确定、checksum 可校验、已有行和文件可复用。
- PG 需要独立备份；dataset SQLite 冻结后可直接文件级备份。
- PG 不可用时，已构建 dataset 仍可本地读取；arcset 构建、校验、挂载不可用。

## 8. 挂载

- dataset 挂载：只支持未吸收的 dataset，要求 shard 仍在 `dataset.metadata.shard_root`（未设置时 `current_path`）；dataset SQLite 只读。
- arcset 挂载：读 PG 的 arcset/relation/shard，再按 `dataset_ref.sqlite_path` 打开各 dataset SQLite 的 file/segment；跨库在应用层 join。`sqlite_path` 必须是当前节点可访问的路径。
- `t_fs_mount` 在 PG；`fs mount` / `fs umount` / `fs list` 都访问 PG。PG 不可用时已挂载实例继续运行，新挂载失败。
- 读取缺失 shard 返回 `ENOENT`；详见 [fuse.md](fuse.md)。

## 9. 待确认

1. PG 的部署/备份/高可用方案（后续再定）。
