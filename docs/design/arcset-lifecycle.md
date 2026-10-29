# Arcset 生命周期与工作流

> 术语见 [glossary.md](glossary.md)。packfs 只使用“分区组 / 分区 / 条带”，不感知下游介质类型。
> 命令与 DB 字段见 [commands-db.md](commands-db.md)；两层数据库见 [storage-architecture.md](storage-architecture.md)；大文件 EC 见 [ec-streaming.md](ec-streaming.md)。

## 1. 定义与容量

```sh
packfs arcset create --name=arc1 --ec=8+4 --groups=2 --layout=raid3 \
                     --partition-width=2 --seq-width=3 \
                     --partition-capacity=18TB --shard-max-bytes=...

# 无 EC：只指定 k
packfs arcset create --name=arc2 --ec=8 --partition-capacity=18TB ...
```

- `ec=k[+m]`：k 个数据分片 + m 个校验分片；不写 `+m` 表示 `m=0`（无 EC）。
  - `k` 范围 1..24；`m` 为 0 或 `{2,4,6}`；工程约束 `k+m ≤ 24`。
- `groups`：分区组数 `group_count`，缺省 1；每组 k+m 个分区（m=0 时为 k 个）。
- `layout`：分区布局，`raid3`（固定位置，默认）或 `raid5`（数据/校验逐条带旋转）；创建后不可修改，变更需新建 arcset。`m=0` 时只接受 `raid3`。
- `partition-width`：partition 目录数字位数，默认 2，最大 3；创建后不可修改。
- `seq-width`：文件名中条带序号的零填充位数，默认 3，允许 1..5；创建后不可修改，固定宽度，超过范围直接报错。
- `partition-capacity`：单个分区的容量；packfs 不关心它是磁带还是磁盘，也不关心是否压缩。
- 容量：
  - 可用数据容量（规划值） = `k × group_count × partition_capacity`
  - 原始容量 = `(k+m) × group_count × partition_capacity`；`m=0` 时两者相等。
- 每个 partition 维护：
  - `partition_capacity`：总容量；
  - `used_capacity`：已写入 shard 的实际大小（压缩后）；
  - `available_capacity = partition_capacity - used_capacity`。
- shard 压缩率不同，`available_capacity` 会随写入变化；容量判断以实际 shard 大小为准，不以 dataset 原始字节数为准。
- 当前 group 放不下时尝试下一 group；所有 group 无空间，或单个 shard 超过目标分区 `available_capacity` 时，直接报错，由人工执行 `arcset expand` 后重试。不自动扩组。

## 2. 两阶段命名

### dataset 阶段（无分区前缀）

data shard 文件名不受限制：

- 内置模式：`0000.tar`、`0001.tar` 等标准序号名；
- 脚本模式：用户自定义文件名，只要是单文件名（不含路径分隔符）即可。

dataset 只识别“不带前缀”的文件名；shard 格式为 `bin` / `tar` / `iso`。`iso` 保留在 packfs 仓库中，可继续使用；不做特殊处理。

### arcset 阶段（partition 目录 + seq 前缀）

采用 **partition 目录 + 文件名 seq 前缀**：

```text
<arcset.current_path>/
├── 00/
│   ├── 000D-astro-0000.tar    # 数据
│   ├── 001E.tar                # EC
│   └── 002P.tar                # PAD（0 字节）
├── 01/
│   └── ...
└── arcset.json          # export 产物，按需生成
```

- **partition 目录**：`partition-width` 位零填充数字，默认 2 位（`00..99`），最大 3 位（`000..999`）。`partition = group_no × (k+m) + pos`，全局从 0 开始。
- **文件名**：
  - 数据：`<seq>D-<dataset_key>-<dataset 原 shard 文件名>`
  - EC：`<seq>E.<ext>`
  - PAD：`<seq>P.<ext>`（0 字节）
- **每个 partition 目录内 `seq` 必须递增且唯一**；同一 stripe 在该 partition 只有一个 shard（DATA/EC/PAD）。
- `m=0` 时只有 DATA 文件，不生成 EC/PAD；最后一条带不足 k 个也合法。
- `seq`：分区组内条带序号，0-based；按 `seq-width` 零填充，默认 3，最大 5。
  - **固定宽度**：超过 `10^seq-width - 1` 直接报错，不自然增位；否则字典序会乱，影响串行介质写入顺序。
- `dataset_key`：arcset 内唯一，`[A-Za-z0-9_]+`；缺省 `ds<dataset_uid 前 8 位>`（如 `ds3f2a1c9b`），可显式指定；冲突直接报错。
- `pos`：条带内位置，0..k-1 数据，k..k+m-1 校验。
- `partition` 按 layout 计算：
  - **raid3**：`group_no × (k+m) + pos`。
  - **raid5**：`group_no × (k+m) + ((pos + seq) % (k+m))`。
- 布局只影响 partition 目录，不影响文件名格式；`D/E/P` 始终表示 shard 角色。
- 示例（k=8, m=4, group 0, partition-width=2, seq-width=3）：
  - raid3, seq=0：`00/000D-astro-0000.tar`、`08/000E.tar`、`05/000P.tar`。
  - raid5, seq=1：数据 pos=0 → partition 1 → `01/001D-astro-...`；EC pos=8 → partition 9 → `09/001E.tar`。
- 拷贝到串行介质时，直接拷贝一个 partition 目录的内容并去掉目录层；文件名以 `seq` 开头，按字典序即为写入顺序。
- 解析规则：
  - 数据：`^(\d{2,3})/(\d+)(D)-([A-Za-z0-9_]+)-(.+)$`
  - EC/PAD：`^(\d{2,3})/(\d+)([EP])\.(.+)$`
  - 目录宽度按 `partition_width`、seq 宽度按 `seq_width` 校验；排序依赖固定宽度。
- `partition_count > 10^partition_width - 1` 时直接报错；2 位（99）在 k+m≤24、4 组（96）内够用，超过则创建时选 3 位。

raid5 布局示例（k=4, m=2, k+m=6，每行一个 stripe）：

```text
seq=0  D D D D E E
seq=1  E D D D D E
seq=2  E E D D D D
seq=3  D E E D D D
seq=4  D D E E D D
seq=5  D D D E E D
seq=6  D D D D E E
```

布局对比：

| layout | 读取路径 | 空间利用 | 默认 |
|--------|----------|----------|------|
| raid3 | 正常只读数据分区；出错才读 EC 分区 | EC 集中在专用分区，可能浪费容量 | ✅ |
| raid5 | 可并发读多个分区；按条带轮转 | EC 与数据共享分区，EC 大小取同条带最大 shard，利用率通常更高 | — |
- 数据 shard 的 `seq` 直接写在文件名中，用于定位同一个 stripe；条带内 `pos` 仍由 DB/manifest 记录。
- manifest 必须随 shard 集合一起移交；DB/manifest 中同时保留 `name`、`original_name`、`dataset_key`、`seq`、`pos`。
- EC/PAD 的 `<ext>` 使用同条带数据 shard 的格式与压缩后缀（如 `.tar`、`.tar.zst`）。
- 解析：
  - 数据：`^D([0-9]+)-([0-9]+)-([A-Za-z0-9_]+)-(.+)$`
  - EC/PAD：`^([EP])([0-9]+)-([0-9]+)\.(.+)$`
  - 原文件名可包含 `-`，但不得含路径分隔符。
- 取出/回传时去掉 partition 目录和 `<seq>D-<dataset_key>-` 前缀即可还原 dataset 原名。
- 不做存量命名兼容；新版本从零开始。

## 3. dataset_key 的选择

- 缺省：`ds<dataset_uid 前 8 位>`，如 `ds3f2a1c9b`；跨库稳定，不需要名称清洗。
- 显式指定：`arcset append --dataset-key=astro`；按 `[A-Za-z0-9_]+` 校验。
- 唯一性：arcset 内唯一；冲突直接报错。
- key 在 append 时确定并写入 `r_arcset_dataset_ref` 与 manifest；之后不随 dataset 名称变化。
- manifest / DB 保存 `dataset_key → dataset_uid/name` 映射，取出后可还原。
- 需要更好的可读性时显式指定名称；缺省不再使用名称清洗。

### 3.1 dataset 生命周期与冻结

- 每个 dataset 一个 SQLite 文件 `<dataset_uid>.db`，保存 `t_dataset` / `t_file` / `t_segment` / `t_shard`（仅 DATA）。
- `t_dataset.status`：`building` → `built`；build 完成 = 所有 DATA shard 已生成并写入 checksum；`--gen-only` 由全部 `shard make` 完成后执行 `dataset mark-built` 置 `built`。
- `arcset append` 在 PG 注册 dataset 并分配 `dataset_key` / `append_order` 后，冻结 dataset SQLite；冻结后只能新建 dataset。
- 不记录 relation 的 registered/absorbed 状态；build-data 是否完成由 DATA shard 数量判断。
- 一个 dataset SQLite 只加入一个 arcset；需要第二份归档时重新创建 dataset。

## 4. shard 排序

dataset 增加两个属性，用于从文件名提取排序键：

```text
shard_sort_regex: ^ch(\d+)_(\d+)\.tar$
shard_sort_index: 1,2
```

- `shard_sort_regex`：Go RE2 正则，匹配 dataset 阶段的原始 shard 文件名（不含分区前缀）；捕获组从 1 开始编号。
- `shard_sort_index`：逗号分隔的捕获组编号，按从前到后排序；先按第 1 个、再按第 2 个……
- 比较规则：两侧都是数字时按数值，否则按字符串；全部相等时以 `t_shard.seq` 或原文件名兜底。
- 未设置时，按 `t_shard.seq` / .def 顺序。
- 正则不匹配、捕获组不存在、`shard_sort_index` 为空 → append 时报错。
- 不保留 `group_regex` / `group_index` 别名，统一使用 `shard_sort_*`，避免与“分区组”混淆。

### 4.1 排序的作用边界

- 排序只决定 dataset 内 DATA shard 的逻辑顺序，不在 dataset SQLite 中产生 `partition` / `stripe_seq` / `pos` 等物理位置字段。
- `arcset build-data` 将 relation `append_order` 与每个 dataset 的排序结果合成全局逻辑序列，再映射为目标 arcset 的 `group_no` / `stripe_seq` / `pos` / `partition`。
- dataset 的 `t_shard.seq` 是本地序号，不直接等于 arcset 的条带 `seq`；映射后以 PG `t_shard` 的布局字段为准。
- 排序不改变 shard 内容、checksum 或原文件名；arcset 阶段只增加 partition 目录和文件名前缀。
- 不保证同一 dataset 的 shard 在物理上连续；PAD 复用会使后 append 的 dataset 填入较早的 stripe。
- 首版不支持将已有 dataset 从一个 arcset 迁移到另一个 arcset；需要第二份归档时重新创建 dataset。

## 5. 排序、填充与扩容

### 5.1 逻辑顺序与物理填充

1. dataset 按 append 顺序形成逻辑序列；每个 dataset 内按 `shard_sort_*` 排序，未配置时按 `t_shard.seq` / 原文件名兜底。
2. 没有可复用 PAD 时，从 group 0 开始按 stripe `seq=0,1,2...` 顺序创建新条带；group 0 用完再 group 1。
3. 有 PAD 时，物理填充优先复用最早 group 的最早未满 stripe；因此后 append 的 dataset 可能占用较早的物理位置，这是预期行为。

### 5.2 条带与分区分配

1. 每个 stripe 取 k 个 data shard，按顺序放入 `pos=0..k-1`；`partition` 按 `layout` 计算（见 §2）：raid3 为 `group_no×(k+m)+pos`，raid5 为 `group_no×(k+m)+((pos+seq)%(k+m))`。
2. `m>0` 时，当前 stripe 不足 k 个用 PAD（0 字节）补齐缺失位置；`m=0` 时不补 PAD，最后一条带可不足 k，后续 append 直接填充空位。PAD 不消耗 `used_capacity`。
3. data shard 构建完成后按实际压缩后大小决定分区；arcset 模式下 `shard_max_bytes` 必须 > 0 且 ≤ `partition_capacity`。
4. 条带 `padded_size` = 该条带最大 data shard 大小；EC shard 大小 = `padded_size`，较小的 data shard 在 EC 计算时补零。
5. 放置一个 stripe 前，同时检查其 k 个数据分区和 m 个 EC 分区的 `available_capacity`（`m=0` 时只检查 k 个数据分区）；不足则尝试下一 group，所有 group 无空间则报错，人工 `arcset expand`。
6. 每个 shard 落位后按实际大小更新分区 `used_capacity` / `available_capacity`；DATA 与 EC 计入，PAD 为 0。
7. 当前 group 放不下下一个完整 stripe 时，使用下一 group；所有 group 无空间则报错，等待人工扩容。
8. 一个 partition 可容纳多个 stripe 的 shard；`seq` 在 group 内递增。
9. 条带 / `seq` / `pos` 统一 0-based；新设计不考虑现有 DB 的 1-based 兼容，旧库通过重建迁移。

### 5.3 PAD 复用与条带重算（仅 m>0）

`m=0` 时不存在 PAD 复用与 EC 重算：直接填充最早 group 的最早未满条带。

新增 dataset 时：

1. 优先填充最早 group 的最早未满 stripe 的 PAD 位置；若新 shard 放不进目标分区 `available_capacity`，直接报错，人工 `arcset expand` 或调整后再试。
2. 被填充的 stripe 必须整体重算 EC：读入 k 个数据位置（含新 shard，仍缺的按零），重新生成 m 个 EC shard。
3. 重算后更新 DATA/EC 分区的 `used_capacity` / `available_capacity`（EC 大小可能变化）。
4. 新数据填满已有 PAD 后，继续创建新 stripe；所有 group 无空间则报错，等待人工扩容。
5. packfs 不实现流水线调度；每个 stripe 的 EC 操作必须幂等，外部调度器可并行/重试。

### 5.4 持久化

不新增 `t_partition` 表：

- `partition_capacity` 存在 arcset metadata；
- `used_capacity` 由 `SUM(t_shard.file_size) WHERE arcset=? AND partition=?` 计算。
- `available_capacity = partition_capacity - used_capacity`；
- `partition_count = group_count × (k+m)`，由 `group_count` 推导。

这样避免容量字段与 shard 记录不一致；`used_capacity` 随 shard 增删自动更新。EC 任务表/重试/并行度属于外部流水线，不在 packfs 内设计。

### 5.5 扩容

```sh
packfs arcset expand --id=1 --add-groups=2
```

- 增加 `group_count`，追加 `add-groups × (k+m)` 个 partition；已有 shard 与 stripe 不变。
- 新数据先复用旧 group 的 PAD，再使用新 group。
- `layout` 不随扩容改变。
- 必须显式执行；没有空间时直接报错，不自动扩容。
- `health != ok` 时不允许扩容。

## 6. 状态模型（分层）

状态按存储层归属，不做跨层统一状态机：

| 对象 | 存储 | 状态 |
|------|------|------|
| dataset SQLite | 本地 SQLite | `t_dataset.status`：`building` / `built`；DATA shard 行存在即 `built` |
| arcset | PG | `status` / `build_phase` / `health` / `parity_degraded` |
| arcset shard | PG | 行存在即已写入；`missing` / `corrupt` 由校验与 health 表达 |
| 介质 | 下游 | `on_media`，packfs 不记录 |

| 字段 | 取值 | 说明 |
|------|------|------|
| `status` | `defined` / `building` / `completed` | 生命周期主状态 |
| `build_phase` | `data` / `ec` / `none` | 仅 `status=building` 有效 |
| `health` | `ok` / `degraded` / `failed` | 数据健康/恢复状态 |
| `parity_degraded` | true / false | 数据完整但 EC shard 缺失 |

不再有 `encoded` / `complete` / `ready` / `finalize`。校验通过即 `completed`；`completed` 仍可 append/expand。

### 6.1 status 与 build_phase

```text
defined
  │ append dataset
  ▼
building (phase=data)
  ├── m=0：make-ec（数据校验）──────────────────> completed
  │
  └── m>0：make-ec
        ▼
      building (phase=ec)
        │ 全部 stripe + 全量校验通过
        ▼
      completed
  │ append
  └──> building (phase=data) ──> ... ──> completed
```

| status / phase | 含义 |
|----------------|------|
| `defined` / `none` | 参数已定义，无 dataset |
| `building` / `data` | 数据 shard 构建中，append 允许 |
| `building` / `ec` | EC/PAD 构建中（仅 `m>0`）；append 返回 busy |
| `completed` / `none` | 当前内容全量校验通过；可 append/expand |

- append 在 `defined`、`building/data`、`completed` 允许；`building/ec` 返回 busy，由 router 在 make-ec 完成后重试；`completed` 时回到 `building/data`。
- expand 只增加 `group_count` 和 partition，不改变 `layout`、已有 shard；`completed` 下 expand 后仍是 `completed`。

### 6.2 health

| health | 含义 |
|--------|------|
| `ok` | 数据 shard 完整且校验通过；EC 缺失时用 `parity_degraded` 单独标记 |
| `degraded` | 存在缺失/损坏 shard，但每组可用 shard ≥ k，可恢复 |
| `failed` | 某组可用 shard < k，无法恢复 |

```text
ok ──validate 失败（≥k）──> degraded ──recover──> ok
                                  │
                                  └──<k──> failed
failed ──外部补齐 shard──> degraded
```

- `health` 与 `status` 正交：例如 `status=completed, health=degraded` 表示当前内容校验有问题。
- `m=0` 时没有 EC，任意 DATA shard 缺失都会使某组可用 shard 数 `< k`，直接置 `failed`，不存在 `degraded`→`recover` 路径。
- `health != ok` 时暂停 append / expand / make-ec，只允许 validate / recover。
- `--target=data`：`health=ok`，`parity_degraded=true`；`status` 保持 `completed`。
- `--target=all`：`health=ok`，`parity_degraded=false`。

### 6.3 操作矩阵

| status / phase | append | expand | make-ec | validate | export | recover |
|----------------|:--:|:--:|:--:|:--:|:--:|:--:|
| defined / none | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| building / data | ✅ | ✅ | ✅ | ✅（数据阶段） | ❌ | ✅ |
| building / ec | 忙，稍后重试 | ✅ | ❌ | ✅（EC 阶段） | ❌ | ✅ |
| completed / none | ✅（回 building/data） | ✅ | ❌ | ✅ | ✅ | ✅ |

- `export` 是操作，不是状态：生成 `arcset.json`，可重复执行。
- `health != ok` 时暂停所有写操作，只允许 validate / recover。

### 6.4 `completed` 的语义

`completed` 表示**当前内容**已通过全量校验，不是封存：

- 仍可 append / expand；append 会回到 `building/data`，完成后重新 `completed`。
- 下游应在 `completed` 后导入；arcset 发生变化后需要重新导入。
- 不再有 `finalize` / `ready`。原 finalize 只解决“校验 + 复制 DB + 封存”，既然校验后即 `completed`，且 arcset 允许继续增长，封存状态没有必要。
- 需要移交产物时执行 `arcset export`，生成 `arcset.json`；它不是状态转换。

## 7. 阶段与校验

1. **append dataset**
   - 要求 dataset SQLite `status=built`；在 PG 注册 `t_dataset_ref` 与 `r_arcset_dataset_ref`，分配 `dataset_key` / `append_order`。
   - 注册成功后冻结 dataset SQLite；冻结后只能新建 dataset。
   - 容量在 build-data 时按实际 shard 大小检查；append 只注册关系，不移动文件。
   - 若 arcset 为 `status=building, phase=ec`，append 返回 busy，由 router 在 make-ec 完成后重试。
   - 若 arcset 为 `status=completed`，append 回到 `building/data`，先复用 PAD 位置并重算受影响 stripe。
2. **build data**
   - 直接使用 dataset SQLite 中已构建的 DATA shard，不再重新按 def 生成。
   - 计算 placement，移动文件到 partition 目录，然后写 PG `t_shard` 行（DATA）。
   - 单次 dataset→arcset 使用**移动**（数据文件大，避免复制）；冻结后不能重新生成 dataset，失败按目标路径 + checksum 幂等重试。
   - 一个 dataset 只加入一个 arcset；需要第二份归档时重新创建 dataset。
   - lazy 路径：`shard make --arcset-id` 直接输出到 partition 目录，并按 `<seq>D-<dataset_key>-<原名>` 命名。
   - 每批完成后做数据阶段校验：文件存在、size、checksum、shard 数。
3. **make-ec（packfs 只提供幂等操作）**
   - `m=0` 时没有 EC/PAD 可生成：校验数据 shard 后直接置 `status=completed`、`build_phase=none`、`health=ok`。
   - `m>0` 时 packfs 不实现流水线编排；按 stripe 的 EC 生成/重算命令必须幂等，外部调度器可重复调用。
   - `m>0` 单次调用处理一个或一批 stripe：按 [ec-streaming.md](ec-streaming.md) 分块读 k 个数据位置（PAD 按零）→ 生成 m 个 EC + PAD → 流式校验。禁止整 shard 读入内存。
   - 同一 stripe 重跑覆盖同一 `seq` 的 EC 文件；失败处理由外部调度器负责。
   - 全部 stripe 完成并通过全量校验后置 `status=completed`、`build_phase=none`、`health=ok`。
   - EC 阶段校验：条带 k+m 个位置齐全、padded_size 一致，使用 [ec-streaming.md](ec-streaming.md) 的流式 `VerifyStripe`。
4. **validate → completed**
   - 全量校验：数据阶段 + EC 阶段 + 各 dataset 的 `shard_count`；通过后 `status=completed`、`health=ok`。
   - `m=0` 时 `--stage=ec` 报错；`--stage=all` 只做数据阶段校验和 `shard_count` 检查。
5. **export（按需，不是状态）**
   - 从 PG 生成 `arcset.json`；可重复执行，arcset 变化后重新导出。
   - 导出不改变 `status`；下游在 `completed` 后导入，arcset 变化后重新导入。

## 8. 取回后校验与 recover

- 下游取回 shard 到工作目录后，packfs 用 `arcset validate` 重新校验。
- 每条带 k+m 个 shard，至少需要 k 个可用（数据或校验均可）才能恢复。
- `m=0` 时任意 DATA shard 缺失即不可恢复（可用 shard 数 `< k`），直接置 `failed`；`--target` 无区别。
- 恢复能力：
  - `--target=all`：恢复缺失/损坏的全部 k+m 个 shard（数据 + EC）。
  - `--target=data`：只恢复 k 个数据 shard，EC 可暂不恢复。
- 恢复流程：
  1. 从 PG 定位条带与位置，列出缺失/损坏 shard。
  2. 检查组内可用 shard 数 ≥ k；否则置 `health=failed`。
  3. 按 [ec-streaming.md](ec-streaming.md) 分块 RS 解码重建；重建后逐个校验 SHA-256，必须与 PG 记录一致；不一致视为恢复失败。
  4. 成功后：
     - `--target=all`：`health=ok`，`parity_degraded=false`；`status` 保持 `completed`。
     - `--target=data`：`health=ok`，`parity_degraded=true`；`status` 保持 `completed`；EC shard 缺失，后续可再执行 `--target=all` 补齐。
- checksum 不因 recover 改变；只更新 PG 的 `last_check` 和 arcset `health`。
- dataset SQLite 保持冻结，不写入 recover 结果。
- PAD 缺失按全零处理，可直接重新生成空文件，不必从下游取回。
- recover 只操作工作目录中的文件，不感知介质；文件由下游按命名放置。
- 若 arcset 已导出，recover 完成后重新生成 `arcset.json`，并通知下游同步修复后的 shard；数据组织不变。

## 9. 导出（arcset 范围）

`arcset export` 按需生成移交产物：

1. 从 PG 读取 arcset、dataset 关系、DATA/EC/PAD shard 清单，生成 `arcset.json`。
2. manifest 记录 `dataset_uid`、`dataset_key`、`sqlite_path` 和 shard 清单。
3. 不复制 dataset SQLite，不生成 arcset 范围 `packfs.db`。
4. 不改变 `status`，可重复执行；arcset 变化后重新 export。

不再有 `finalize` 状态转换。

## 10. 待决问题

1. PG 的部署、备份与高可用方案（后续再定）。
