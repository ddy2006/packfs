# packfs 设计文档

> 2026-10-23：缺口与后续功能的概要设计（讨论中）见 [docs/design/](design/README.md)，术语见 [docs/design/glossary.md](design/glossary.md)。数据库分两层：dataset SQLite + 全局 PostgreSQL，见 [design/storage-architecture.md](design/storage-architecture.md)。磁带相关职责归下游存储系统；arcset 状态模型以 [design/arcset-lifecycle.md](design/arcset-lifecycle.md) 为准（`defined` / `building` / `completed` + health）。

## 功能概述

- **打包**：将目录下所有文件打包成多个 shard。支持 bin（二进制拼接）和 tar 两种格式，支持 zstd / xz / zstd_seekable 压缩（shard 级或 segment 级）。记录每个 segment 在 shard 中的位置和大小，支持 `shard_max_bytes` 控制分片粒度。
- **解包**：从 shard 文件恢复原始文件，自动处理压缩。支持 bin 和 tar 两种格式。
- **挂载**：通过 FUSE 只读挂载未吸收的 dataset，或挂载 arcset；arcset 挂载跨 PG 与 dataset SQLite 读取（需要 `segment:*` 或 `zstd_seekable` 压缩才能支持随机读取）。
- **校验**：重算 SHA-256 checksum 与 DB 记录对比；recover 不改变 checksum。
- **导出**：`arcset export` 生成 `arcset.json`，不复制 dataset SQLite。

## 领域模型

```
dataset SQLite ──dataset_uid──> PG t_dataset_ref ──r_arcset_dataset_ref──> t_arcset
       │                                                                       │
       ├── t_shard(DATA) ──> t_segment ──> t_file                              │
       └────────────────────────────── PG t_shard(DATA/EC/PAD) <───────────────┘
```

| 实体 | 存储 | 说明 |
|------|------|------|
| dataset | 每 dataset 一个 SQLite | 不可变文件集合，从磁盘目录递归扫描创建 |
| arcset | PostgreSQL | 多个 dataset 的聚合容器，shard 的父级 |
| shard（dataset 层） | dataset SQLite | 打包后的 DATA shard，含 SHA-256 checksum |
| shard（arcset 层） | PostgreSQL | DATA/EC/PAD 的布局与全局清单 |
| segment | dataset SQLite | shard 内一个片段，对应源文件的某个字节范围 |
| file | dataset SQLite | dataset 内的单个文件记录 |

## 数据库设计

两层存储，完整定义见 [design/storage-architecture.md](design/storage-architecture.md)。

| 层 | 存储 | 内容 |
|----|------|------|
| dataset | 每个 dataset 一个 SQLite `<uid>.db` | `t_dataset` / `t_file` / `t_segment` / `t_shard`（仅 DATA） |
| arcset | 全局 PostgreSQL | `t_dataset_ref` / `t_arcset` / `r_arcset_dataset_ref` / `t_shard`（DATA/EC/PAD） |
| 挂载 | 全局 PostgreSQL | `t_fs_mount` |

- 无跨库外键；逻辑键为 `dataset_uid` + `seq`。
- dataset SQLite build 完成（所有 DATA shard 生成）后 `status=built`；首次 arcset 注册后冻结。
- 不记录 relation 的 absorbed 状态；DATA shard 数量表示 build-data 是否完成。
- 大部分业务字段存在 JSON `metadata` 列中，表结构只保留 id、关联键和核心状态。
- 完整 DDL：`build/sqlite/dataset_schema.sql`、`build/postgres/arcset_schema.sql`。

### dataset SQLite

| 表 | 说明 |
|----|------|
| `t_dataset` | `uid`、name、`status`（`building` / `built`）、metadata、current_path |
| `t_file` | 原始文件路径、大小、checksum、dataset |
| `t_shard` | `seq`、dataset 内原名、`type=DATA`、大小、sha256 |
| `t_segment` | shard 内 offset/size/csize，关联 file 与本地 shard |

dataset metadata：

| 属性 | 说明 |
|------|------|
| num_files | 文件总数量 |
| total_bytes | 数据集总数据量 |
| format | `bin` / `tar` / `iso`，缺省 `bin` |
| compress | `zstd` / `xz` / `segment:zstd` / `segment:xz`；缺省不压缩 |
| shard_max_bytes | shard 最大字节数，0=不限制 |
| shard_root | DATA shard 输出目录；未设置时用 current_path |
| shard_count | shard 数量 |
| shard_sort_regex / shard_sort_index | dataset 内 shard 排序键；只影响逻辑顺序，arcset 位置由 build-data 计算 |

### PostgreSQL

| 表 | 说明 |
|----|------|
| `t_dataset_ref` | 全局 dataset 身份：`uid`、`sqlite_path`、name |
| `t_arcset` | arcset 参数、`status` / `build_phase` / `health` / `parity_degraded` |
| `r_arcset_dataset_ref` | arcset–dataset 关系、`dataset_key`、`append_order` |
| `t_shard` | DATA/EC/PAD 布局、带前缀 `file_path` |

arcset metadata：

| 属性 | 说明 |
|------|------|
| create_time | 创建时间 |
| ec | `"8+4"`；无 EC 为 `"8+0"` |
| group_count | 分区组数 |
| layout | `raid3` / `raid5` |
| partition_width / seq_width | 目录/序号位数 |
| partition_capacity | 单分区容量 |
| data_capacity / raw_capacity | 规划容量 |
| shard_max_bytes | 首次 append 从 dataset 继承 |
| total_bytes / shard_count / sum_bytes / net_bytes | 统计 |

PG shard metadata：`padded_size`、`original_size`、`data_bytes`。

#### file / segment

file metadata：`file_mode`、`ctime`、`mtime`。segment 字段：`offset`、`size`、`csize`、`shard`、`file`、`file_offset`、`file_size`。

## Shard 定义文件（.def）

### 文件名

```
<4位序号>[.<compress>].<format>.def
```

例：`0000.bin.def`、`0001.zst.bin.def`

### 内容格式

```
# dataset_db: /data/datasets/<uid>.db
相对路径/a.txt
相对路径/b.txt
```

- `# dataset_db`：dataset SQLite 路径，由 gen-def 自动写入；`# arcset_id`（可选）：arcset 模式下由 gen-def 写入
- 后续每行一个相对路径（相对于 `dataset.current_path`）
- 一个 shard 不跨多个 dataset
- 也支持 JSON 行：`{"path":"...","offset":0,"size":1024}`，`offset`/`size` 选填

### gen-def：双模式

**1. 内置模式**（缺省）：

```sh
packfs dataset create --root-dir=/data --db-dir=/data/datasets --shard-max-bytes=1073741824
```

`dataset create` 生成 `<uid>.db`，自动扫描目录 → `GenerateShardDefs` 分组 → `MakeShard` 打包，一步到位，不写 .def 文件。

dataset 与目标 arcset 的 `format` / `compress` / `shard_max_bytes` 不一致时，直接新建 dataset；首版不提供 `dataset rebuild`。

**2. 脚本模式**（自定义分组场景）：

```sh
# 第一步：仅扫描
packfs dataset create --root-dir=/data --db-dir=/data/datasets --gen-only

# 第二步：外部脚本生成 .def 文件
bash ./gen-def.sh --dataset-db=/data/datasets/<uid>.db --target-root=./data/def

# 第三步：打包
packfs shard make --def-file=./data/def/0000.tar.def --output-dir=./data/shard
```

- 外部脚本打开指定 dataset SQLite（`--dataset-db`）、查询 `t_file`、生成 .def 文件
- .def 格式需遵守标准：`# dataset_db` 头 + 路径行 / JSON segment 行
- 参考实现：`examples/astro/gen-def.sh`（按 channel 分组，40 文件/shard）

## 压缩

`compress` 参数控制压缩配置：

| 值 | 级别 | 算法 | 支持 mount |
|----|------|------|-----------|
| `""` | — | 无 | — |
| `"zstd"` | shard | zstd | 否 |
| `"xz"` | shard | xz | 否 |
| `"segment:zstd"` | segment | zstd | 是 |
| `"segment:xz"` | segment | xz | 是 |
| `"zstd_seekable"` | shard | zstd seekable | 是 |

## 状态机

```
dataset SQLite:  building ──所有 DATA shard 完成──> built ──首次 arcset 注册──> frozen
arcset:          defined ──> building ──> completed
                                ▲             │
                                └── append ───┘
```

> 注：完整模型见 [design/arcset-lifecycle.md](design/arcset-lifecycle.md)：`status` + `build_phase` + `health` + `parity_degraded`；`completed` 仍可 append/expand，介质状态属于下游存储系统。

| 状态 | 实体 | 含义 |
|------|------|------|
| `building` | dataset SQLite | 文件已扫描，DATA shard 尚未全部完成 |
| `built` | dataset SQLite | 所有 DATA shard 已生成并写入 checksum |
| frozen | dataset SQLite | 首次 arcset 注册后只读 |
| `defined` / `building` / `completed` | arcset | 生命周期状态（`build_phase` 区分阶段） |
| `ok` / `degraded` / `failed` | arcset health | 数据健康/恢复状态，与 status 正交 |

## 纠删码（EC）

基于 Reed-Solomon，将 k 个 data shard 编码为 k+m 个 shard，允许丢失任意 ≤m 个。

**参数约束**：k 范围 1..24；m 为 0 或 `{2,4,6}`；arcset 工程约束 `k+m ≤ 24`。`m=0` 表示不启用 EC，只生成 k 个数据 shard，无 EC/PAD。

**大文件**：100GB 级 shard 必须使用 [design/ec-streaming.md](design/ec-streaming.md) 的分块流式编码/恢复/校验，禁止整 shard 读入内存。

**EC 文件命名**：`<partition>/<seq>D-<dataset_key>-<原名>`（数据）/ `<partition>/<seq>E.<ext>`（校验）/ `<partition>/<seq>P.<ext>`（填充）；partition 目录默认 2 位、最大 3 位，seq 默认 3 位、最大 5 位，0-based；见 [design/arcset-lifecycle.md](design/arcset-lifecycle.md)。

**流程**：
```
1. arcset create --ec=8+4          # 不启用 EC：--ec=8
2. arcset append（注册 dataset SQLite、冻结；校验 format/compress/shard_max_bytes 三项一致）
3. arcset make-ec --id=1
   ├─ m>0：按 stripe 分块 EncodeStripeStreaming → 更新 PG t_shard
   └─ m=0：校验数据 shard → completed
```

**恢复**：
```sh
arcset recover --id=1 --shard-file=00/000D-ds3f2a1c9b-0000.tar
```
定位 stripe/position → 收集存活 shard → 分块 ReconstructStripe → 流式 VerifyStripe。重建后的 SHA-256 必须与原记录一致，不更新 checksum；`m=0` 时任意数据 shard 丢失都无法恢复。

**修改 EC 参数**：首版不实现 `arcset rebuild`；需要新建 arcset 重新构建。

### dataset build 完成

1. 校验所有 DATA shard checksum。
2. 确认 `COUNT(t_shard) >= metadata["shard_count"]`。
3. `t_dataset.status=built`；`--gen-only` 由全部 `shard make` 完成后执行 `dataset mark-built` 置 `built`。
4. dataset SQLite 保持可写；首次 arcset 注册后冻结。

原 `dataset finalize` 取消：不再复制全局 `packfs.db`，dataset SQLite 本身即自包含元数据。

### shard_count 检查

`gen-def` / `dataset create` 写入 `metadata["shard_count"]`，build / validate 执行比较：

| 实际 vs 预期 | 行为 |
|-------------|------|
| 相等 | OK；dataset build 置 `built`，arcset validate all 置 `completed` |
| 实际 > 预期 | warn + 更新 `shard_count`；同上 |
| 实际 < 预期 | 报错拦截 |
| 未设置 | 跳过检查 |

## WebUI 管理面板

`packfs serve --addr=:8080` 启动内嵌 HTTP server，WebUI 编译进二进制，零外部依赖。

### 架构

```
浏览器 ── HTTP ──> packfs serve
                    ├─ /api/*  → REST handler (internal/api/)
                    └─ /*      → 内嵌静态文件 (//go:embed webui)
```

### REST API

```
GET    /api/health                           → 健康检查
POST   /api/datasets                         → 创建 dataset SQLite
GET    /api/datasets/{uid}                   → dataset 详情
POST   /api/datasets/{uid}/unpack            → 解包

GET    /api/arcsets                          → 列表（PG）
POST   /api/arcsets                          → 创建
POST   /api/arcsets/{id}/append              → 注册 dataset、冻结 SQLite
POST   /api/arcsets/{id}/build-data          → 移动 DATA shard
POST   /api/arcsets/{id}/make-ec|validate|recover|expand|export

GET    /api/fs/mounts                        → PG 挂载列表
POST   /api/fs/mount|unmount                 → 挂载/卸载
```

完整端点与缺口见 [design/api-webui.md](design/api-webui.md)。

### 前端

- 纯 HTML/CSS/JS，无框架
- 工作流管道：页面顶部横向展示 `📁 源目录 → 📦 Dataset → 🔧 Shard → 📚 Arcset → 🛡️ EC → 🤝 移交下游存储`
- EC 可视化：data shard 大方块 + EC parity 小方块，丢失/恢复状态颜色区分
- CLI 命令栏：实时显示对应 packfs 命令，可一键复制

### 与 CLI 的关系

WebUI 和 CLI 共用 `internal/` 下的 Store 和业务逻辑，数据完全一致。WebUI 上创建、删除、EC 编码等操作等同于执行 CLI 命令。

## 幂等性

dataset SQLite 中 `shard make` 使用 `INSERT ... ON CONFLICT(dataset, file_path) DO UPDATE`；`t_segment` 使用 `DELETE` + 批量 `INSERT`（事务内）。PG 的 `t_shard` 按 `(arcset, file_path)` 与 `(arcset, group_no, stripe_seq, pos)` 幂等 upsert；build-data 按目标路径 + checksum 重试。
