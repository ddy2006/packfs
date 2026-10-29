# 术语表

> packfs 只使用“分区 / 分区组 / 条带”等逻辑术语，不直接提磁带、磁带组、机械手等下游概念；下表的“下游对应”仅用于对齐，不进入 packfs 代码与正式接口。

| 术语 | 定义 | 下游对应 |
|------|------|----------|
| **arcset** | 归档集，包含 `group_count` 个分区组；`group_count` 可显式扩容 | 一个 arcset 对应多组 k+m 盘介质 |
| **partition group（分区组）** | k+m 个 partition 组成的一组，构成一个完整的 EC 空间；可用数据容量 = `k × partition_capacity` | 一组 k+m 盘磁带 |
| **group_no** | 分区组序号，0-based；`group_no = 0..group_count-1` | — |
| **partition（分区）** | 一份逻辑介质，每个条带在其中放一个 shard；目录位数由 `partition_width` 决定，默认 2 位，最大 3 位；容量 = `partition_capacity` | 一盘磁带/一份介质 |
| **partition_count** | 分区总数 = `group_count × (k+m)` | 介质总数 |
| **stripe（条带）** | 分区组内同一行的 k+m 个 shard：k 个 DATA + m 个 EC；PAD 用于补齐缺失的数据位置。`m=0` 时只有 k 个 DATA，最后一条带可不足 k | 同一行分布在 k+m 盘上的 shard |
| **无 EC（m=0）** | arcset 不启用纠删码：`--ec=k`；无 EC/PAD，`raw_capacity = data_capacity`，`make-ec` 只做数据校验 | 纯数据分区组 |
| **PAD 复用 / 条带重算** | 新增数据优先填入已有 PAD 位置，并对该条带整体重新计算 EC | — |
| **position / pos** | 条带内位置：0..k-1 为数据位置，k..k+m-1 为校验位置 | — |
| **seq** | 分区组内的条带序号，0-based；按 `seq_width` 固定宽度零填充，默认 3 位；超范围报错 | — |
| **seq_width** | arcset 参数：文件名中 `seq` 的零填充位数，默认 3，允许 1..5；创建后不可修改 | — |
| **partition_width** | arcset 参数：partition 目录数字位数，默认 2，最大 3；创建后不可修改 | — |
| **layout** | 分区布局：`raid3`（数据/校验位置固定，默认）或 `raid5`（每 stripe 校验块旋转 1 位）；创建后不可修改 | 固定/轮转分布 |
| **shard** | 一个 shard 文件；dataset 层只有 DATA，arcset 层有 DATA / EC / PAD | — |
| **DATA / EC / PAD** | shard 类型：数据、校验、0 字节填充；`m=0` 时只有 DATA | — |
| **dataset** | 不可变文件集合，生成不带分区前缀的数据 shard | — |
| **dataset SQLite** | 每个 dataset 一个独立 SQLite 文件 `<dataset_uid>.db`，保存 dataset/file/segment/DATA shard；首次注册到 arcset 后冻结 | — |
| **PostgreSQL 全局库** | arcset 层权威库：`t_dataset_ref`、`t_arcset`、`r_arcset_dataset_ref`、`t_shard`（DATA/EC/PAD）、`t_fs_mount` | — |
| **dataset_uid** | dataset 的全局标识，本地生成 UUID；写入 dataset SQLite 和 PG 的 `t_dataset_ref` | — |
| **dataset status** | dataset SQLite 生命周期：`building` / `built`；build 完成 = 所有 DATA shard 生成并写入 checksum；`--gen-only` 全部 `shard make` 完成后执行 `dataset mark-built` 置 `built` | — |
| **frozen（冻结）** | dataset SQLite 首次在 PG 注册成功后转为只读；冻结后不再修改 | — |
| **file** | dataset 中的原始文件 | — |
| **segment** | file 在 shard 内的一个字节区间，记录 offset/size/csize | — |
| **dataset_key** | dataset 在 arcset 内的唯一短标识，用于数据 shard 文件名前缀；缺省 `ds<UID 去连字符后的前 8 位>`（如 `ds3f2a1c9b`），可显式指定；冲突直接报错 | — |
| **shard_sort_regex** | dataset 属性：从 shard 文件名提取排序字段的正则（捕获组）；只决定 dataset 内逻辑顺序，不决定 arcset 物理位置；不使用 `group_regex` 别名 | — |
| **shard_sort_index** | dataset 属性：逗号分隔的捕获组编号，决定排序优先级；不使用 `group_index` 别名 | — |
| **partition_capacity** | 单个 partition 的容量 | 单盘磁带容量 |
| **used_capacity** | partition 已写入 shard 的实际大小（压缩后），由 `SUM(t_shard.file_size)` 计算 | — |
| **available_capacity** | partition 剩余容量 = `partition_capacity - used_capacity`；随压缩后 shard 大小变化 | — |
| **EC chunk** | 流式 EC 的固定分块大小，默认 8MiB；内存约 `(k+m) × chunk` | — |
| **data_capacity** | 可用数据容量（规划值） = `k × group_count × partition_capacity` | — |
| **raw_capacity** | 原始容量（规划值） = `(k+m) × group_count × partition_capacity`；`m=0` 时等于 `data_capacity` | — |
| **expand（扩容）** | 增加 `group_count`，追加 partition，扩大 arcset 容量；不改变已有 shard | 增加一组 k+m 盘介质 |
| **prefix** | partition 目录（默认 2 位）+ 文件名 seq 前缀：`00/000D-...` / `00/001E.<ext>` / `00/002P.<ext>` | — |
| **status** | arcset 生命周期：`defined` / `building` / `completed` | — |
| **build_phase** | `building` 下的阶段：`data` / `ec` / `none` | — |
| **health** | arcset 健康状态：`ok` / `degraded` / `failed` | — |
| **completed** | 当前内容全量校验通过；仍可 append / expand，不是封存态 | — |
| **parity_degraded** | `--target=data` 恢复后的标记：数据完整但 EC shard 有缺失 | — |
| **export** | 按需生成 `arcset.json`；不改变 status | — |
| **on_media** | 下游存储系统记录的介质写入状态；packfs 不记录 | 已写入介质 |
| **stripe operation** | 按 stripe 执行的 EC 生成/重算操作，幂等；调用/重试由外部调度器负责 | — |

## 命名冲突提醒

- `group` 在本项目中有两个含义：**分区组**与**正则捕获组**。文档中提到“分组数”时统一写 `group_count`；dataset 排序属性使用 `shard_sort_regex` / `shard_sort_index`，不使用裸 `group`。
- 不使用：`tape layout prefix`（改为 partition prefix）、`tape group`（改为 partition group）、`tape count`（改为 partition_count）、`tape capacity`（改为 partition_capacity）。
- `taped` / `ON_TAPE` / barcode / slot / drive 属于下游状态，不进入 packfs 状态机。
