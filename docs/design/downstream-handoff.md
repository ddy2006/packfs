# packfs → 下游存储移交接口

> packfs 只知道“下游存储系统”，不出现下游产品名。
> 术语见 [glossary.md](glossary.md)：partition group / partition / stripe，不使用 tape group 等下游概念。

## 1. 职责边界

| 能力 | packfs | 下游存储系统 |
|------|:--:|:--:|
| 文件→shard、压缩、EC 编解码 | ✅ | 调用 packfs |
| arcset 自包含目录 + arcset.json | ✅ | 读取/导入 |
| 分区/分区组分配、介质定位 | ❌ | ✅ |
| 写入/取回、介质校验、故障恢复调度 | ❌ | ✅ |
| shard→partition 映射 | 通过命名表达 | 按命名落库 |

packfs 不实现介质/磁带管理命令、不维护 `T_tape`/`R_shard_tape`、不记录 `taped` 状态，也不关心介质是磁带还是磁盘、是否压缩。

## 2. 解耦方式

- **shard 文件名**决定 shard 属于哪个 partition；下游按命名规范解析。
- **arcset.json** 提供 EC 参数、shard 清单、checksum、dataset 关联、条带顺序等元数据。
- packfs 不感知 barcode、槽位、驱动器；下游不感知文件/segment 内部结构。

## 3. 命名契约

### dataset 阶段

data shard 文件名不受限制：标准序号（`0000.tar`）或用户脚本自定义文件名均可，不带 partition 前缀。

### arcset 阶段

采用 **partition 目录 + seq 前缀**：

```text
00/
  000D-astro-0000.tar    # 数据
  001E.tar                # EC
  002P.tar                # PAD
01/
  ...
```

- partition 目录：`partition_width` 位零填充数字，默认 2（`00..99`），最大 3（`000..999`）；`partition = group_no × (k+m) + pos`。
- 每个 partition 目录内 `seq` 必须递增且唯一。
- 文件名：
  - 数据：`<seq>D-<dataset_key>-<dataset 原 shard 文件名>`
  - EC：`<seq>E.<ext>`
  - PAD：`<seq>P.<ext>`
- `seq`：分区组内条带序号，0-based；固定宽度 `seq_width`（默认 3，允许 1..5）；超过 `10^seq_width-1` 报错，不自然增位。
- `dataset_key`：arcset 内唯一，`[A-Za-z0-9_]+`；缺省 `ds<dataset_uid 前 8 位>`（如 `ds3f2a1c9b`），可由 `--dataset-key` 显式指定；冲突直接报错。
- `pos`：0..k-1 数据位置，k..k+m-1 校验位置。
- `layout` 默认 `raid3`；`partition` 全局从 0 开始，`group_no` 为分区组序号（0-based）：
  - `raid3`：`partition = group_no × (k+m) + pos`。
  - `raid5`：`partition = group_no × (k+m) + ((pos + seq) % (k+m))`。
- 数据 shard 的 `seq` 写在文件名中；`pos` 由 DB/manifest 记录。
- manifest 必须随 shard 集合一起移交；文件名本身不足以确定 EC 条带的完整组成。
- EC/PAD 的 `<ext>` 使用同条带数据 shard 的格式与压缩后缀（如 `.tar`、`.tar.zst`）。
- `m=0` 时只有数据文件，不生成 EC/PAD；最后一条带可不足 k 个数据 shard。
- PAD：`type=PAD`，文件 0 字节；`padded_size` 记录零填充长度。下游需要把 PAD 也写入对应 partition。仅 `m>0` 时存在。
- 解析：
  - 数据：`^(\d{2,3})/(\d+)(D)-([A-Za-z0-9_]+)-(.+)$`
  - EC/PAD：`^(\d{2,3})/(\d+)([EP])\.(.+)$`
  - 原文件名可包含 `-`，但不得含路径分隔符。
- 示例（k=8, m=4, group 0, partition_width=2, seq_width=3）：
  - raid3, seq=0：`00/000D-astro-0000.tar`、`08/000E.tar`、`05/000P.tar`。
  - raid5, seq=1：数据 pos=0 → partition 1 → `01/001D-astro-...`；EC pos=8 → partition 9 → `09/001E.tar`。
- 拷贝到串行介质时，直接拷贝一个 partition 目录的内容并去掉目录层；文件名以 `seq` 开头，按字典序即写入顺序。
- 取出/回传时去掉 partition 目录和 `<seq>D-<dataset_key>-` 前缀还原原文件名。

存量命名不考虑；新版本从零开始。

## 4. 文件进入 arcset

- 单次 dataset→arcset 使用**移动**；packfs 每次只处理一个 arcset。
- 一个 dataset 只加入一个 arcset；需要第二份归档时重新创建 dataset。
- 不记录 relation 的 absorbed 状态；DATA shard 全部存在即表示已移入 arcset。
- 首次 append 注册后，dataset SQLite 冻结；之后只读。
- lazy 路径：`shard make --arcset-id` 直接输出到 partition 目录，并按 `<seq>D-<dataset_key>-<原名>` 命名。

## 5. arcset.json（最小字段）

```json
{
  "manifest_version": 1,
  "arcset": {
    "name": "...",
    "ec": {"k": 8, "m": 4},
    "layout": "raid3",
    "partition_width": 2,
    "seq_width": 3,
    "group_count": 2,
    "partition_capacity": 0,
    "data_capacity": 0,
    "raw_capacity": 0,
    "shard_count": 0,
    "status": "completed",
    "health": "ok",
    "parity_degraded": false
  },
  "partitions": [{
    "partition": 0,
    "group_no": 0,
    "pos": 0,
    "partition_capacity": 0,
    "used_capacity": 0,
    "available_capacity": 0
  }],
  "datasets": [{
    "uid": "...",
    "key": "ds3f2a1c9b",
    "name": "...",
    "sqlite_path": "/data/datasets/<uid>.db",
    "shard_count": 0,
    "shard_sort_regex": "^ch(\\d+)_(\\d+)\\.tar$",
    "shard_sort_index": "1,2"
  }],
  "shards": [{
    "name": "00/000D-ds3f2a1c9b-0000.tar",
    "original_name": "0000.tar",
    "dataset_uid": "...",
    "dataset_key": "ds3f2a1c9b",
    "type": "DATA",
    "partition": 0,
    "group_no": 0,
    "pos": 0,
    "seq": 0,
    "size": 0,
    "sha256": "...",
    "padded_size": 0,
    "original_size": 0
  }]
}
```

- 数据 shard 同时保留 `name`、`original_name`、`dataset_uid`、`dataset_key`，便于取出后还原；示例中的 `ds3f2a1c9b` 是缺省 key，也可显式指定为 `astro`。
- EC/PAD 的 `seq` 必须与同条带的数据 shard 顺序一致；数据 shard 的全局顺序由 `seq` 表达。
- `m=0` 时 `ec.m=0`，`shards` 中没有 EC/PAD 条目；`raw_capacity` 等于 `data_capacity`，`parity_degraded` 恒为 false。
- `partitions` 记录每个分区的 `partition_capacity` / `used_capacity` / `available_capacity`；`used_capacity` 按已写入 shard 的实际压缩后大小累计，PAD 不计入。
- `parity_degraded=true` 表示数据完整但 EC shard 有缺失（`--target=data` 恢复后的状态）。
- recover 不改变 checksum；manifest 中的 `sha256` 始终是构建时的值。
- append / expand 会更新 manifest，并重算受影响 stripe 的 EC shard；下游应在 `status=completed` 后导入，arcset 变化后重新导入。
- `arcset.json` 由 PG 生成，不复制 dataset SQLite；file/segment 明细留在原 dataset SQLite。
- 下游只写介质时，`arcset.json` + shard 文件即可；需要解包/FUSE 时，另行访问 dataset SQLite。

## 6. 容量与分区

- arcset 定义 `group_count` 与 `partition_capacity`；可用数据容量规划值 = `k × group_count × partition_capacity`。
- 实际可用容量按每个 partition 的 `available_capacity` 计算；压缩后 shard 大小不同，容量会变化。
- `partition_count = group_count × (k+m)`，partition 全局连续编号；`m=0` 时为 `group_count × k`。
- 填充顺序：group 0 的 stripe 0..N 依次填满，再 group 1；所有 group 用满后可 `expand` 增加 `group_count`。
- 新增 dataset 时优先复用 PAD 位置，并重算受影响 stripe 的 EC；下游不应在 `completed` 前导入。
- 下游按 `partition` 将 shard 分配到具体介质；一个 partition 对应一份介质。
- 下游负责容量校验与装载顺序；packfs 只保证命名与清单一致。

## 7. 接口风险

1. **dataset_key 唯一性**：同一 arcset 内必须唯一；缺省或显式 key 冲突都直接报错。
2. **PAD 类型**：仅 `m>0` 时存在；下游若只支持 DATA/EC，需要增加 PAD 或按 manifest 的 `type` 处理；PAD 文件为 0 字节。
3. **注册入口**：下游需要 import manifest 的 API/CLI；packfs 先提供 `arcset.json`。下游若可直接访问 PG，可另走全局查询，但 JSON 仍是契约。
4. **checksum 双写**：manifest 与下游 shard 记录的 checksum 必须一致；写入后由下游复核。recover 不改变 checksum。
5. **EC 恢复**：下游故障恢复流程调用 packfs 的 recover；需要约定取回目录与目标（data/all）。
6. **顺序信息**：data shard 文件名包含 `seq`，但条带内 `pos` 仍需查 manifest/DB；同一条带的数据 shard 顺序不能仅靠文件名恢复。
7. **布局**：下游按 partition 目录落位；拷贝到介质时去掉目录层。`layout`、`seq_width`、`pos` 由 manifest 提供，用于校验和恢复。

## 8. 待决问题

1. 下游是否接受 `(arcset, name)` 复合主键？
2. 注册入口用 manifest 文件还是 HTTP API？是否需要直接查询 PG？
3. 多组时 partition 全局连续编号是否可行？
4. 原文件名是否允许包含 `-`、空格、非 ASCII？解析只依赖固定字段。
5. 下游在 arcset `completed` 后导入；arcset 发生变化后重新导入。
6. 下游是否需要随 manifest 提供 dataset SQLite 路径，还是只需要 `arcset.json` + shard 文件？
