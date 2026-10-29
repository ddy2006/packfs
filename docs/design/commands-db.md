# 命令与 DB 字段细化

> 配合 [arcset-lifecycle.md](arcset-lifecycle.md)、[storage-architecture.md](storage-architecture.md) 阅读。
> dataset 元数据在每 dataset 一个 SQLite；arcset 元数据在全局 PostgreSQL。不引入 pipeline/task 表。
> 旧库不兼容；dataset SQLite schema 与 PG schema 分别重建。

## 实现状态（2026-10-29）

- 已实现：`arcset create/append/build-data/make-ec/validate/recover/export/list`、`dataset create/mark-built/unpack`、新的 `shard make`（`# dataset_db`）。
- 待实现：`arcset show/expand`、`--ec-parallel`、API/WebUI；`shard make-ec/recover` 和 `dataset list` 仍是旧路径。
- 当前 `arcset make-ec` 支持 `--ec-chunk-size`、`--group`、`--seq`；`arcset validate` 暂不支持 `--source-root`，`arcset recover` 暂不支持 `--group/--source-root`。

## 1. 命令总览

| 命令 | 作用 |
|------|------|
| `arcset create` | 定义 arcset：EC、group_count、layout、seq_width、partition_capacity |
| `arcset append` | 注册 dataset SQLite，确定 dataset_key / append_order，冻结 SQLite |
| `arcset build-data` | 排序 data shard、分配 partition/stripe、移动文件并写 DB |
| `arcset make-ec` | 按 stripe 生成/重算 EC + PAD；支持 `--group/--seq` |
| `arcset validate` | 数据阶段 / EC 阶段 / 全量校验，更新 health |
| `arcset expand` | 显式增加 group_count；无空间时由人工执行（待实现） |
| `arcset recover` | 按 stripe 恢复 data / all |
| `arcset export` | 显式导出 `arcset.json` |
| `arcset list` | 查询 status/build_phase/health/parity_degraded、容量、shard 统计 |
| `arcset show` | 单 arcset 详情（待实现） |
| `dataset create` | 创建独立 dataset SQLite，构建 DATA shard，增加 shard 排序属性 |
| `dataset mark-built` | 校验 DATA shard 并置 `built`；用于 `--gen-only` + 外部 `shard make` |
| `shard make-ec` | 旧低层路径，待迁移 |

`arcset rebuild` 暂不纳入首版：改 EC 参数需要全量重排，避免与 append/expand 冲突；后续单独设计。

跨 arcset 迁移（dataset 从一个 arcset 移到另一个 arcset，含 EC 重建）暂不纳入首版；后续有需求再单独设计。

## 2. 命令细节

### 2.1 `arcset create`

```sh
packfs arcset create \
  --name=arc1 --target-root=/data/arc1 \
  --ec=8+4 --groups=2 --layout=raid3 \
  --partition-width=2 --seq-width=3 \
  --partition-capacity=18000000000000 \
  [--shard-max-bytes=...] [--label=...] [--comment=...]

# 无 EC：只指定 k
packfs arcset create --name=arc2 --target-root=/data/arc2 \
  --ec=8 --partition-capacity=18000000000000 ...
```

- `--ec` 必填，语法 `k[+m]`；不写 `+m` 表示 `m=0`（无 EC）。
  - `k` 1..24；`m` 为 0 或 `{2,4,6}`；工程约束 `k+m ≤ 24`。
  - metadata 存规范形式：有 EC 为 `"8+4"`，无 EC 为 `"8+0"`。
- `--groups` 缺省 1；`group_count`。
- `--layout` 缺省 `raid3`；创建后不可修改；`m=0` 时只接受 `raid3`。
- `--partition-width` 缺省 2，最大 3；创建后不可修改。
- `--seq-width` 缺省 3，允许 1..5；创建后不可修改，超过上限报错。
- `--partition-capacity` 必填，单分区容量（字节）。
- `--shard-max-bytes` 可选；未指定时首次 append 从 dataset 继承，必须 >0 且 ≤ `partition_capacity`。
- `partition_count = group_count × (k+m)`，必须 ≤ `10^partition_width - 1`；超出报错（2 位最多 99，3 位最多 999）。
- 不再使用 `--tape-count` / `--tape-max-bytes`。

PG：

- 插入 `t_arcset`：`status=defined`、`build_phase=none`、`health=ok`、三个 flag=0。
- metadata 写入：`ec`、`group_count`、`layout`、`partition_width`、`seq_width`、`partition_capacity`、`data_capacity`、`raw_capacity`、`create_time`。
  - `data_capacity = k × group_count × partition_capacity`；
  - `raw_capacity = (k+m) × group_count × partition_capacity`，`m=0` 时与 `data_capacity` 相等。
- 输出 `{"arcset_id":1,"status":"defined","partition_count":24,...}`。

### 2.2 `arcset append`

```sh
packfs arcset append --id=1 --dataset-db=/data/datasets/<uid>.db [--dataset-key=astro]
```

- 读取 dataset SQLite，要求 `t_dataset.status=built`、所有 DATA shard 已生成。
- `format` / `compress` / `shard_max_bytes` 三项必须与 arcset 已继承的值一致；任一不一致直接报错，需新建 dataset。
- arcset 尚未继承参数时（首次 append），从该 dataset 继承 `format` / `compress` / `shard_max_bytes`。
- 重复 append 同一 dataset 到同一 arcset：若 relation 已存在，返回已有 `dataset_key`，不新增。
- 一个 dataset 只允许加入一个 arcset；若该 `dataset_uid` 已存在其他 arcset 的 relation，直接拒绝。
- `dataset_key` 缺省为 `ds<dataset_uid 前 8 位>`（如 `ds3f2a1c9b`）；冲突直接报错。
- `append_order` = 当前最大值 + 1。
- 执行顺序：
  1. PG 创建或复用 `t_dataset_ref`（`uid` / `sqlite_path`），插入 `r_arcset_dataset_ref`；
  2. PG 成功后冻结 dataset SQLite（只读）；重试时若 relation 已存在，补做冻结；
  3. 返回。
- 冻结后只能新建 dataset。
- 不立即检查容量；容量在 build-data 时按实际 shard 大小检查。
- 状态：
  - `defined` / `building/data` / `completed` → `building/data`。
  - `building/ec` → 返回 busy，由 router 在 make-ec 完成后重试。
  - `health != ok` → 拒绝。

PG：

- `INSERT INTO t_dataset_ref(uid, name, sqlite_path, ...)`（首次）。
- `INSERT INTO r_arcset_dataset_ref(arcset, dataset_ref, dataset_key, append_order)`。
- 更新 `t_arcset.status/build_phase`。
- 输出 `{"arcset_id":1,"dataset_uid":"...","dataset_key":"astro","append_order":0}`。

### 2.3 `arcset build-data`

```sh
packfs arcset build-data --id=1 [--dataset-uid=<uuid>]
```

- 要求 `status=building, build_phase=data`，`health=ok`；`shard_max_bytes > 0` 且 ≤ `partition_capacity`。
- 对每个已注册 dataset relation：
  1. 读取 dataset SQLite 的 DATA shard 清单；不重新生成 shard。
  2. 按 `shard_sort_regex` / `shard_sort_index` 排序；未配置按 `t_shard.seq` / 文件名。排序只决定 dataset 内逻辑顺序，`t_shard.seq` 不直接作为 arcset 条带序号。
  3. 逻辑顺序 = relation `append_order` → dataset 内排序；物理位置优先复用已有 PAD，再创建新 stripe。
  4. 依次填入 stripe：`pos=0..k-1`，`partition` 按 layout 计算。
  5. 每个 shard 按实际大小检查目标 partition 的 `used_capacity`；放不下直接报错，不自动扩容。
  6. 移动 shard 到 arcset 的 partition 目录，按 `<seq>D-<dataset_key>-<原名>` 命名；写入 PG `t_shard` 行（DATA）。
  7. `m>0` 时，若目标位置已有 PAD，直接把 PAD 行更新为 DATA，并删除该 stripe 的 EC 行、置 `parity_degraded=1`；后续 make-ec 重算该 stripe。PAD 复用按 `group_no, stripe_seq, pos` 最早优先。`m=0` 时无 PAD，直接填充未满条带。
- 全部 DATA shard 写入 PG 后，该 dataset 的 build-data 完成（不记录额外 relation 状态）。
- 崩溃重试：目标路径确定；已存在的目标文件按 checksum 校验，已存在的行复用，缺失的行补写。
- 输出 `{"built":N}`。

### 2.4 `arcset make-ec`

```sh
packfs arcset make-ec --id=1 [--ec-chunk-size=8MiB] [--group=0] [--seq=3]
```

- 要求 `status=building`、`build_phase=data` 或 `ec`、`health=ok`，且所有已注册 dataset 的 DATA shard 已写入 PG。
- `m=0`：不生成 EC/PAD，也不进入 `build_phase=ec`；校验 DATA shard 后置 `status=completed`、`build_phase=none`、`health=ok`、`parity_degraded=0`。
- `m>0` 首次调用将 `build_phase` 置 `ec`。
- 大文件必须使用 [ec-streaming.md](ec-streaming.md) 的分块流式 API；不得用 `os.ReadFile` 整 shard 读入。
- 对每个待处理 stripe：
  1. 若 `seq >= 10^seq_width`，直接报错，要求增大 `seq_width` 或拆 arcset。
  2. 按 `stripe_seq/pos` 取 k 个 data shard；缺失位置生成 PAD（0 字节）。
  3. 生成 m 个 EC shard；命名 `<partition>/<seq>E.<ext>`、`<partition>/<seq>P.<ext>`（partition 宽度按 `partition_width`）。
  4. 写/更新 PG `t_shard`（EC/PAD：`dataset_ref` 为空）。
  5. 使用 [ec-streaming.md](ec-streaming.md) 的流式 `VerifyStripe` 校验 stripe。
- 全部 stripe 完成并全量校验通过后：`status=completed`、`build_phase=none`、`health=ok`、`parity_degraded=0`。
- `arcset make-ec --group/--seq` 只处理一个 stripe；不指定时处理全部 stripe。旧 `shard make-ec` 尚未迁移到新路径。`m=0` 时不做 EC。

### 2.5 `arcset validate`

```sh
packfs arcset validate --id=1 [--stage=data|ec|all] [--source-root=/data/arcset]
```

- `data`：校验 DATA shard 存在、size、sha256。允许 `building/data`。
- `ec`：要求所有 DATA shard 已写入；校验 EC/PAD、stripe 完整、padded_size 一致，使用 [ec-streaming.md](ec-streaming.md) 的流式 `VerifyStripe`；通过后清 `parity_degraded`。允许 `building/ec` / `completed`。`m=0` 时直接报错。
- `all`：要求所有 DATA shard 已写入；数据 + EC + 各 dataset `shard_count`；全过 → `status=completed`、`health=ok`。`m=0` 时只校验数据阶段和 `shard_count`，不检查 EC。
- 失败：
  - 每组可用 shard ≥ k → `health=degraded`。
  - 某组 < k → `health=failed`。
- 只读检查，不修改 shard 内容。

### 2.6 `arcset expand`

```sh
packfs arcset expand --id=1 --add-groups=2
```

- 要求 `status` 为 `defined` / `building` / `completed`，`health=ok`。
- metadata `group_count += N`；`data_capacity` / `raw_capacity` / `partition_count` 重算。
- 新 `partition_count` 必须 ≤ `10^partition_width - 1`；超出报错。
- 不新建表；partition 由 `group_count × (k+m)` 推导。
- 已有 shard 不变；`layout` 不变。
- 无空间时不会自动调用；必须显式执行本命令。

### 2.7 `arcset recover`

```sh
packfs arcset recover --id=1 [--group=0] [--target=data|all] [--source-root=...] [--ec-chunk-size=8MiB]
```

- 要求 `health=degraded` 或 `failed`；`status` 通常为 `completed`。
- 每条带至少 k 个可用 shard；否则保持 `failed`。
- `target=all`：恢复缺失 data + EC；`health=ok`、`parity_degraded=0`。
- `target=data`：只恢复 data；`health=ok`、`parity_degraded=1`。
- 使用 [ec-streaming.md](ec-streaming.md) 的分块恢复；重建后逐个校验 sha256，必须与 PG 记录一致；不一致视为恢复失败，不更新 checksum。
- 成功后更新 PG 的 `last_check` 和 arcset `health`；dataset SQLite 保持冻结。
- 已 export 的 arcset 需要重新 export 并通知下游。

### 2.8 `arcset export`

```sh
packfs arcset export --id=1 [--target-root=/data/arcset-export]
```

- 要求 `status=completed`、`health=ok`；`parity_degraded` 允许为 true，并在 manifest 记录。
- 从 PG 生成 `arcset.json`；不复制 dataset SQLite。
- manifest 记录 `dataset_uid`、`dataset_key`、`sqlite_path`、DATA/EC/PAD shard 清单。
- 不生成 arcset 范围 `packfs.db`；不改变 status；可重复执行。
- 输出 `{"manifest":"..."}`。

### 2.9 `arcset list` / `show`

- 查询 PG；`list` 支持 `--status`、`--health`、`--name` 过滤；输出 id/name/status/build_phase/health/group_count/容量。
- `show --id` 输出 metadata、dataset relation、partition 使用量（按 PG `t_shard` 聚合）、stripe 完成度、`parity_degraded`。

### 2.10 `dataset create`

```sh
packfs dataset create --root-dir=/data/src \
  --db-dir=/data/datasets [--target-root=/data/shards] \
  --shard-sort-regex='^ch(\d+)_(\d+)\.tar$' \
  --shard-sort-index=1,2
```

- 本地生成 `dataset_uid`（UUID），创建独立 SQLite `<db-dir>/<uid>.db`；`--db-dir` 缺省 `$PACKFS_DATASET_DB_DIR`，未设置时 `~/data/packfs/datasets`。
- `--target-root` 为 DATA shard 输出目录，写入 `metadata.shard_root`；未指定时沿用现有规则。
- 扫描源目录 → `t_file` → DATA shard；全部完成后 `t_dataset.status=built`；`--gen-only` 时保持 `building`；外部 `shard make` 全部完成后执行 `dataset mark-built` 置 `built`。
- `shard_sort_regex` / `shard_sort_index` 写入 `t_dataset.metadata`；未设置时按 `t_shard.seq` / 文件名。
- 其他参数（format/compress/shard_max_bytes）保持不变。
- 不访问 PG；输出 `{"dataset_uid":"...","sqlite_path":"...","shard_count":N}`。

### 2.11 `dataset mark-built`

```sh
packfs dataset mark-built --dataset-db=/data/datasets/<uid>.db
```

- 读取独立 dataset SQLite，校验全部 DATA shard 的存在与 SHA-256。
- 写入 `metadata.shard_count`，置 `t_dataset.status=built`；不复制 SQLite、不访问 PG。
- 已 `built` 的 dataset 重复执行幂等。
- 输出 `{"dataset_uid":"...","status":"built","shard_count":N}`。

## 3. DB 字段

### 3.1 dataset SQLite

完整 DDL 见 [`build/sqlite/dataset_schema.sql`](../../build/sqlite/dataset_schema.sql)。每个 dataset 一个 `<dataset_uid>.db`，只存 dataset 层数据。

| 表 | 关键字段 |
|----|----------|
| `t_dataset` | `id`、`uid`、`name`、`status`、`metadata`、`current_path` |
| `t_file` | `id`、`file_path`、`file_size`、`metadata`、`sha256`、`dataset` |
| `t_shard` | `id`、`seq`、`file_path`、`file_size`、`type=DATA`、`sha256` |
| `t_segment` | `offset`、`size`、`csize`、`shard`、`file`、`file_offset`、`file_size` |

- `t_dataset.status`：`building` → `built`；build 完成 = 所有 DATA shard 生成并写入 checksum。
- 冻结通过只读文件权限/连接实现，不新增状态。
- metadata：`format`、`compress`、`shard_max_bytes`、`shard_root`、`shard_sort_regex`、`shard_sort_index`、`shard_count`、`total_bytes`。

### 3.2 PostgreSQL

`t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard` 定义见 [storage-architecture.md](storage-architecture.md#5-postgresql-schema)。要点：

- `t_dataset_ref`：`uid`、`sqlite_path`、name、metadata。
- `r_arcset_dataset_ref`：`dataset_key`、`append_order`；`UNIQUE(dataset_ref)` 保证一个 dataset 只属于一个 arcset。
- `t_shard`：DATA/EC/PAD；DATA 带 `dataset_ref`、`seq`、`original_name`、`file_size`、`sha256` 快照；EC/PAD 的 `dataset_ref` 为空。
- PG 不保存 `t_file` / `t_segment`。

metadata（`t_arcset.metadata`）：

| key | 说明 |
|-----|------|
| `ec` | `"8+4"`；无 EC 为 `"8+0"`（规范形式） |
| `group_count` | 分区组数 |
| `layout` | `raid3` / `raid5`；`m=0` 时固定 `raid3` |
| `partition_width` | 默认 2，最大 3 |
| `seq_width` | 默认 3，最大 5 |
| `partition_capacity` | 单分区容量 |
| `data_capacity` | `k × group_count × partition_capacity` |
| `raw_capacity` | `(k+m) × group_count × partition_capacity`；`m=0` 时等于 `data_capacity` |
| `format` / `compress` | 首次 append 继承 |
| `shard_max_bytes` | 首次 append 继承 |
| `total_bytes` / `shard_count` / `sum_bytes` / `net_bytes` | 统计 |
| `create_time` | RFC3339 |

### 3.3 `t_fs_mount`（PostgreSQL）

`t_fs_mount` 放全局 PostgreSQL；dataset SQLite 只读，不使用本地状态库。完整 DDL 见 [`build/postgres/arcset_schema.sql`](../../build/postgres/arcset_schema.sql)。

| 字段 | 说明 |
|------|------|
| `mount_point` | 挂载点，唯一 |
| `type` | `dataset` / `arcset` |
| `dataset_uid` | dataset 挂载时使用；逻辑引用，不要求已注册 |
| `arcset` | arcset 挂载时使用，引用 `t_arcset` |
| `create_time` | 创建时间 |

### 3.4 废弃表

- `T_tape`、`R_shard_tape` 从 packfs schema 删除；介质由下游管理。
- 旧全局 `packfs.db` 的 dataset/arcset/fuse 混合表废弃，按两层 schema 重建。

### 3.5 Store 扩展

- dataset 层：`SQLiteStore` 按 SQLite 文件打开；增加 `CreateWithUID`、`UpdateStatus`、`FindShardBySeq`、`DeleteShards`、`ListShards`。
- PG 层：新增 `PGStore`，实现 `arcset.Store`、`arcset.ShardStore`、`dataset.RefStore`。
- `arcset.Store`：`Create`、`UpdateState`、`FindByID/Name`、`List`、`AddDatasetRef`、`FindDatasetRef`。
- `arcset.ShardStore`：`Insert`、`Update`、`FindByStripe`、`FindByStripePos`、`SumPartitionUsed`、`ListByArcset`。
- `dataset.RefStore`：`Register`、`FindByUID`、`FindBySQLitePath`。
- `mount.Store`（PG）：`Create`、`Delete`、`List`、`FindByMountPoint`。

## 4. 命令与状态矩阵

| 命令 | defined | building/data | building/ec | completed | 备注 |
|------|:--:|:--:|:--:|:--:|------|
| create | 产出 | — | — | — | |
| append | ✅ | ✅ | 忙，稍后重试 | ✅ | health=ok |
| build-data | — | ✅ | — | — | |
| make-ec | — | ✅ | ✅ | — | 可单 stripe |
| validate | — | ✅ | ✅ | ✅ | stage 区分 |
| expand | ✅ | ✅ | ✅ | ✅ | 显式；health=ok |
| recover | — | — | — | ✅ | health=degraded/failed；checksum 不变 |
| export | — | — | — | ✅ | 显式；只导出 JSON |

- `append` 额外要求 dataset SQLite `status=built`；注册成功后冻结 SQLite。
- `build-data` 移动文件后写 PG `t_shard`；崩溃后按目标路径 + checksum 重试。
- 同一 arcset 的控制面写操作由 scalebox router 串行调用；packfs 不内置 arcset 锁，router 实现与故障恢复由 scalebox 项目负责。

## 5. 实施顺序

1. EC 参数：`ec.ParseConfig` 支持 `k`（m=0），`ec.Validate` 允许 m=0，工程约束改为 `k+m ≤ 24`。
2. dataset SQLite schema + 本地 Store（每 dataset 一个文件，`uid` / `status`）。
3. dataset create：`--db-dir`、UUID、`shard_sort_*`、`status=built`。
4. PG schema + `PGStore`（`t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard`）。
5. arcset create / list / show。
6. arcset append（注册 dataset、分配 dataset_key/append_order、冻结 SQLite、三项一致性校验）。
7. arcset build-data（读取 dataset SQLite、移动、写 PG `t_shard`）。
8. arcset validate --stage=data。
9. EC 流式 API：`EncodeStripeStreaming` / `ReconstructStripeStreaming` / `VerifyStripeStreaming`，见 [ec-streaming.md](ec-streaming.md)。
10. arcset make-ec（含 m=0 分支）+ validate --stage=ec/all → completed。
11. arcset expand。
12. arcset export（只导出 JSON）。
13. arcset recover（checksum 不变，只更新 PG）。
14. FUSE：dataset 挂载限制、arcset 跨库挂载、PG `t_fs_mount`。
15. API/WebUI 与测试。

## 6. 待定

- PG 的部署、备份、高可用与连接配置。
- `arcset rebuild`（修改 EC k+m）是否保留；首版不实现。
- `arcset delete` 的级联范围（PG 与 arcset 目录）。
- `t_arcset.name` 唯一约束是否需要兼容同名旧数据（新库无此问题）。
