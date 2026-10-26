# packfs 概要设计（讨论稿）

> 2026-10-23 起，缺口与后续功能先在此做概要设计。每篇只回答“现状 → 目标 → 设计要点 → 待决问题”，确认后再实现。
> **文档约定**：packfs 单向面向“下游存储系统”，不感知其具体实现，也不出现其产品名；讨论稿同样只使用“下游存储系统”。

## 模块边界

- **packfs**：文件 → dataset SQLite → shard → EC → arcset（completed）+ arcset.json。只做数据处理与命名，不碰下游介质硬件。
- **下游存储系统**：arcset 注册 → 分区/介质分配 → 写入/取回 → 校验/故障恢复。
- **解耦接口**：shard 文件命名 + `arcset.json`。命名决定 shard 属于哪个分区（介质）；packfs 不感知 barcode、槽位、驱动器。
- packfs 的 `T_tape`、`R_shard_tape`、`arcset.taped` 属于历史遗留，标记废弃，后续迁移删除。

## 文档索引

| 文档 | 内容 | 优先级 | 状态 |
|------|------|--------|------|
| [storage-architecture.md](storage-architecture.md) | 两层数据库：dataset SQLite + 全局 PostgreSQL | P0 | 已确认 |
| [ec-streaming.md](ec-streaming.md) | 100GB shard 的流式 EC 编解码/恢复/校验 | P0 | 已确认 |
| [glossary.md](glossary.md) | 分区/分区组/条带等术语统一 | P0 | 讨论中 |
| [arcset-lifecycle.md](arcset-lifecycle.md) | arcset 定义、容量模型、构建/校验/恢复状态机、命名 | P0 | 讨论中 |
| [commands-db.md](commands-db.md) | 命令细化、DB 字段与索引、实施顺序 | P0 | 讨论中 |
| [downstream-handoff.md](downstream-handoff.md) | 命名契约、manifest、分区映射 | P0 | 讨论中 |
| [fuse.md](fuse.md) | mount/unmount/fsck、arcset 挂载、T_fs_mount | P0 | 讨论中 |
| [api-webui.md](api-webui.md) | API 桩、缺失操作、WebUI 页签 | P0 | 讨论中 |
| [packaging.md](packaging.md) | 压缩校验、输出目录、gen-def、并行、ISO、scalebox | P1 | 讨论中 |
| [platform.md](platform.md) | PostgreSQL、测试、文档同步、补全 | P1 | 讨论中 |

## 本轮确认的设计方向

1. packfs 不感知下游实现，shard 命名是唯一的分区接口。
2. arcset 定义包含 EC `k+m`（工程约束 `k+m ≤ 24`）、`group_count`、单分区介质容量；可用数据容量 = `k × group_count × partition_capacity`。packfs 不关心介质是磁带还是磁盘、是否压缩。
3. 状态机覆盖：定义 → 构建数据 shard → 构建 EC shard → 校验通过 `completed` → 取回后校验 / recover；`completed` 仍可 append / expand。
4. `arcset append` 要求 dataset SQLite `status=built`，不要求其他 finalize；构建过程中可边 build 边加入（lazy）。
5. PAD shard 要写下游介质（仅 `m>0`）；packfs 侧生成 0 字节空文件。
6. 命名分两阶段：dataset 阶段文件名不受限制；arcset 阶段用 partition 目录（默认 2 位、最大 3 位）+ seq 前缀：`<partition>/<seq>D-<dataset_key>-<原名>`、`<partition>/<seq>E.<ext>`、`<partition>/<seq>P.<ext>`；每个 partition 目录内 seq 唯一递增。
7. `dataset_key` 缺省为 `ds<dataset_uid 前 8 位>`（如 `ds3f2a1c9b`），可通过 `--dataset-key` 显式指定；冲突直接报错。
8. dataset 增加 `shard_sort_regex` / `shard_sort_index` 属性，从 shard 文件名提取排序键；无别名，无配置时按 `seq`。
9. 条带 `seq` / `pos` 统一 0-based。
10. 每个 partition 计算 `available_capacity`（剩余容量），按 shard 实际压缩后大小更新。
11. 新增 dataset 时优先复用 PAD 位置，并对受影响的整个 stripe 重新计算 EC。
12. 填充顺序：先填满 group 0 的 stripe 0..N，再填 group 1；全部用满时直接报错，必须显式 `arcset expand`。
13. 单次 dataset→arcset 使用**移动**；一个 dataset 只加入一个 arcset。需要多份归档时，重新创建 dataset；是否保留原始数据作为后续可选功能，不在首版实现。
14. arcset 增加 `layout`（默认 `raid3` 固定 / `raid5` 每 stripe 旋转 1 位）、`partition_width`（默认 2，最大 3）、`seq_width`（默认 3，允许 1..5）；两者创建后不可修改，超上限报错；`seq` 写入数据 shard 文件名。
15. 新设计统一 0-based，不兼容现有 DB 的 1-based，旧库通过重建迁移。
16. raid5 容量与 PAD 复用已确认；EC shard 大小取同条带最大 shard。
17. `layout` 创建后不可修改，变更需新建 arcset。
18. 取消 `finalize`；`arcset export` 是显式按需命令，只生成 `arcset.json`。
19. `--target=data` 恢复后：`status=completed`，`health=ok`，置 `parity_degraded=true`。
20. `building/ec` 期间 append 返回 busy，由 router 在 make-ec 完成后重试；不记录排队标志。
21. packfs 暂不实现流水线编排；EC 操作按 stripe 幂等，供外部调度器调用。
22. arcset 状态采用分层模型：`status`（`defined` / `building` / `completed`）/ `build_phase` / `health` / `parity_degraded`。
23. `completed` 后可继续 append/expand，下游在 arcset 变化后重新导入。
24. `--ec` 支持 `k`（无 EC，`m=0`）和 `k+m`；无 EC 时不生成 EC/PAD，最后一条带可不足 k，`make-ec` 只做数据校验并置 `completed`。
25. append 要求 dataset 与 arcset 的 `format` / `compress` / `shard_max_bytes` 三项一致；不一致时报错，需新建 dataset。
26. 首版不实现 `dataset rebuild`；参数不一致时重新创建 dataset。
27. FUSE 支持 dataset / arcset 只读挂载；dataset 仅未吸收且 shard 就位；arcset 根目录为 `<dataset_key>/`；arcset 挂载要求 PG 与 dataset SQLite 可访问，DB 有记录即可列目录，读取时 shard 缺失返回 `ENOENT`。
28. 存储分两层：每个 dataset 一个 SQLite 文件（file/segment/DATA shard），arcset 元数据放全局 PostgreSQL；无跨库外键，只用 `dataset_uid` + `seq` 逻辑引用。
29. `dataset_uid` 本地生成 UUID；SQLite 文件名 `<uid>.db`；PG 的 `t_dataset_ref` 记录 `uid` / `sqlite_path`；`dataset_key` 缺省取 UID 去连字符后的前 8 位。
30. build 完成 = 所有 DATA shard 构建完成，`t_dataset.status=built`；`--gen-only` 由 `shard make` 完成全部 shard 后置 `built`；首次 arcset 注册成功后冻结 SQLite。
31. PG 的 `t_shard` 保存 DATA/EC/PAD；DATA 的 `file_size` / `sha256` 是从 dataset SQLite 复制的快照；行存在即已写入。
32. 不记录 relation 的 registered/absorbed 状态；DATA shard 数量表示 build-data 是否完成。
33. recover 重建的 DATA shard checksum 必须与记录一致；只更新 PG 的 `last_check` 和 arcset `health`。
34. `arcset export` 产物只有 `arcset.json`；不复制 dataset SQLite，不生成 arcset 范围 `packfs.db`。
35. dataset 挂载只支持未吸收的 dataset；吸收后只能 arcset 挂载。
36. `t_fs_mount` 放全局 PostgreSQL；dataset SQLite 只读，不再使用本地状态库。
37. EC 编码/恢复/校验必须分块流式，禁止整 shard 读入内存；默认 chunk 8MiB，内存约 `(k+m) × chunk`。
38. 同一 arcset 的控制面操作由 scalebox router 串行化；packfs 不内置 arcset 锁，数据面 stripe 可并行。router 实现与故障恢复由 scalebox 项目负责，不在 packfs 范围。
39. 跨节点时 `sqlite_path` 只需对 router/执行节点可见，由 scalebox 负责共享或同步；packfs 不做跨节点复制。
40. 冻结与注册非原子可接受：append 幂等、重试补冻结；放弃时人工恢复权限或新建 dataset。

## 待确认的核心问题

1. PG 的部署、备份与高可用方案（后续再定）。
