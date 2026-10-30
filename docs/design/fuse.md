# FUSE 挂载完善

## 现状

- 已实现 `fs mount --dataset-db` / `--arcset-id`、`fs list`、`fs umount`；挂载记录写 PG `t_fs_mount`。
- dataset 索引读独立 SQLite；arcset 索引按 PG DATA shard 的 `file_path` 跨库拼路径，根目录为 `<dataset_key>/`。
- 物理 shard 缺失时读取返回 `ENOENT`。
- `fs fsck` 仍是未实现桩；索引构建已有单元测试。2026-10-30 在 macOS 14 + osxfuse（2019）环境尝试实际挂载，go-fuse 在 `getConnection` 阻塞，未完成运行验证。
- 旧 `fs mount --dataset-id` 和本地状态库路径已废弃。
- 支持矩阵：bin 无压缩 / `segment:zstd` / `segment:xz` 可随机读；shard 级 `zstd`/`xz` 需全量解压；`zstd_seekable` 未实现。

## 目标

- 只读挂载 dataset 或 arcset；dataset 仅支持未吸收，arcset 支持吸收后跨库读取。
- 挂载记录落 PG，支持 list/umount/fsck。
- DB 有记录即可挂载、列出目录；物理 shard 未就位时读取报错。

## 设计要点

### 1. 挂载对象

```sh
packfs fs mount --dataset-db=/data/datasets/<uid>.db --mount-point=/mnt/pk
packfs fs mount --arcset-id=1 --mount-point=/mnt/pk
```

- `--dataset-db` / `--arcset-id` 互斥，必须指定其一。
- dataset 挂载：根目录直接是该 dataset 的文件树；要求 DATA shard 仍在 `dataset.metadata.shard_root`（未设置时 `current_path`）。
- dataset 已吸收（shard 已移走）时拒绝挂载，提示改用 arcset 挂载。
- arcset 挂载：根目录为 `<dataset_key>/`，每个关联 dataset 一个子目录；文件路径 `/mnt/pk/<dataset_key>/<file_path>`。
- `dataset_key` 来自 PG `r_arcset_dataset_ref`，arcset 内唯一。

### 2. dataset 挂载

- 打开指定的 dataset SQLite（只读），读取 `t_file` / `t_segment` / `t_shard`。
- `SegmentLoc.ShardPath = dataset.metadata.shard_root（未设置时 current_path）+ t_shard.file_path`。
- 挂载前检查所有 DATA shard 是否存在；缺失说明 dataset 已吸收或文件未就位，拒绝挂载并提示使用 `--arcset-id`。
- 读取数据不依赖 PG；但 `fs mount` 需要在 PG `t_fs_mount` 写记录，PG 不可用时新挂载失败。

### 3. arcset 挂载

- 从 PG 读取 arcset、`r_arcset_dataset_ref`、`t_dataset_ref`、`t_shard`。
- 按每个 relation 的 `dataset_ref.sqlite_path` 打开 dataset SQLite（只读），构建 file → segment 索引。
- 逻辑 join：
  1. dataset SQLite 的 `t_segment.shard` → 本地 `t_shard.id` → `seq`；
  2. PG `t_shard` 按 `(arcset, dataset_ref, seq)` 找到 DATA 行；
  3. `ShardPath = arcset.current_path + PG t_shard.file_path`。
- EC/PAD 不参与文件读取索引。
- 目录树只来自 dataset SQLite 的 `t_file`；不同 dataset 的同名 `file_path` 由 `dataset_key` 目录隔离。
- 打开多个 dataset SQLite 时保持只读连接，卸载时统一关闭。
- `dataset_ref.sqlite_path` 必须是当前节点可访问的路径；跨节点挂载时需要共享存储或本地副本。
- PG 不可用时 arcset 挂载失败；首版不做 `arcset.json` 回退。dataset 挂载不受影响。

### 4. 离线挂载与读取错误

- 挂载和列目录不检查物理 shard 是否存在（dataset 挂载的前置整体检查除外）。
- `Open` 时检查该文件所有 segment 对应的 shard 是否存在；缺失返回 `ENOENT`，应用层看到 “No such file or directory”。
- 同时记录日志：缺失 shard 的解析路径。
- 目录项仍显示；文件是否可读取决于 shard 是否就位。
- 不自动 recover；需要 `arcset validate` / `arcset recover`。

### 5. T_fs_mount（PostgreSQL）

`T_fs_mount` 放全局 PostgreSQL；dataset SQLite 只读，不使用本地状态库。完整 DDL 见 [`build/postgres/arcset_schema.sql`](../../build/postgres/arcset_schema.sql)。

关键字段：`mount_point`（唯一）、`type`（`dataset` / `arcset`）、`dataset_uid`、`arcset`、`create_time`。

- mount 成功后插入 PG 记录；umount 成功后删除记录。
- 进程退出时删除记录；启动时可清理 stale 记录。
- `fs list` 从 PG 列出 mount_point/type/dataset_uid/arcset/create_time。

### 6. umount / fsck

- `fs umount --mount-point`：调用 OS 卸载（Linux `fusermount -u`，macOS `umount`）；成功后删除 PG `t_fs_mount` 记录，失败保留记录并报错。
- `fs fsck --dataset-db=...` 或 `--arcset-id=...`：检查索引引用的 shard 是否存在、大小是否一致；`--checksum` 时做全量 SHA-256 校验；只读，不修改数据。

### 7. 格式与压缩

- 保持现有支持矩阵；bin 随机读最优。
- `tar` / `iso` 或 shard 级压缩可以挂载，但读取需要全量解压或无法随机定位，命令行给出警告。
- `zstd_seekable` 待压缩方案确定后再支持。

## 待决问题

1. 是否支持多个 dataset 合并挂载（非 arcset）？
2. `fs umount` 是否允许跨机器/跨进程卸载（依赖 OS 命令）？
3. `fsck --checksum` 是否默认全量，还是提供抽样参数？
