# packfs 设计文档

## 功能概述

- **打包**：将目录下所有文件打包成多个 shard。支持 bin（二进制拼接）和 tar 两种格式，支持 zstd / xz / zstd_seekable 压缩（shard 级或 segment 级）。记录每个 segment 在 shard 中的位置和大小，支持 `shard_max_bytes` 控制分片粒度。
- **解包**：从 shard 文件恢复原始文件，自动处理压缩。支持 bin 和 tar 两种格式。
- **挂载**：通过 FUSE 挂载 arcset 为只读文件系统（需要 `segment:*` 或 `zstd_seekable` 压缩才能支持随机读取）。
- **校验**：重算 SHA-256 checksum 与 DB 记录对比。
- **封存**：finalize 后目录自包含（DB + shard），可独立分发。

## 领域模型

```
dataset ──(r_arcset_dataset)──> arcset ──> shard ──> segment ──> file
```

| 实体 | 表 | 说明 |
|------|-----|------|
| dataset | `t_dataset` | 不可变文件集合，从磁盘目录递归扫描创建 |
| arcset | `t_arcset` | 多个 dataset 的聚合容器，shard 的父级 |
| shard | `t_shard` | 打包后的大文件，含 SHA-256 checksum |
| segment | `t_segment` | shard 内一个片段，对应源文件的某个字节范围 |
| file | `t_file` | dataset 内的单个文件记录 |

## 数据库设计

SQLite 为主，路径由 `SQLITE_DB` 环境变量指定（缺省 `~/data/packfs.db`），首次自动建表（`internal/db/schema.sql` 通过 `go:embed` 嵌入）。也可手动用 `build/sqlite/erd.sql` 创建。

ER 图：![ER 图](../packfs-erd.svg)

支持 PostgreSQL 扩展（`build/postgres/initdb.d/20-erd.sql`）。ER 图见 `packfs-erd.png`。

大部分业务字段（create_time、format、shard_max_bytes 等）存在 JSON `metadata` 列中，表结构仅保留 id、name、关联 FK 等核心列。

### t_dataset

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| name | VARCHAR | |
| label | VARCHAR | |
| status | VARCHAR | `active` / `archived` / `absorbed` |
| metadata | JSON | 含 `num_files`、`total_bytes`、`format`、`compress`、`shard_max_bytes` |
| current_path | VARCHAR | 文件实际存放路径 |
| comment | TEXT | |

### t_arcset

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| name | VARCHAR | |
| label | VARCHAR | |
| status | VARCHAR | `building` / `complete` / `ready` / `taped` |
| metadata | JSON | 含 `tape_max_bytes`、`ec`、`total_bytes` 等 |
| current_path | VARCHAR | shard 输出目录 |
| last_check | DATETIME | |
| comment | TEXT | |

### t_shard

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| seq | SMALLINT | 序号 |
| file_path | TEXT | 相对 current_path 的路径 |
| file_size | BIGINT | |
| type | VARCHAR | DATA / EC / PAD |
| checksum | VARCHAR | SHA-256 |
| metadata | JSON | 含 `stripe`、`position`、`padded_size`、`original_size` 等 |
| last_check | DATETIME | |
| arcset | INTEGER FK | NULLABLE，关联 t_arcset |
| dataset | INTEGER FK | NULLABLE，关联 t_dataset |

CHECK 约束：`dataset IS NOT NULL OR arcset IS NOT NULL`（至少关联一方）。
条件唯一索引：`WHERE dataset IS NOT NULL`（dataset shard 唯一）、`WHERE arcset IS NOT NULL`（arcset shard 唯一）。

### t_segment

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| offset | BIGINT | shard 内偏移 |
| size | BIGINT | 数据大小 |
| csize | BIGINT | 压缩后大小 |
| shard | INTEGER FK | NOT NULL |
| file | INTEGER FK | NOT NULL |
| file_offset | BIGINT | 源文件内偏移 |
| file_size | BIGINT | 源文件大小 |

### metadata 字段定义

#### arcset

| 属性 | 说明 |
|------|------|
| create_time | 创建时间 |
| ec | Erasure Code 参数，例 `8+4` |
| tape_max_bytes | 磁带大小（字节数） |
| shard_max_bytes | shard 最大字节数（首次 append 时从 dataset 继承） |
| shard_count | 预期最少 shard 数 |
| total_bytes | 所有关联 dataset 原始文件总字节数 |
| sum_bytes | 所有 shard 总字节数 |
| net_bytes | 数据 shard 总字节数 |

#### dataset

| 属性 | 说明 |
|------|------|
| num_files | 文件总数量 |
| total_bytes | 数据集总数据量 |
| format | `bin` / `tar` / `iso`，缺省 `bin` |
| compress | 压缩配置。shard 级：`zstd`、`xz`；segment 级：`segment:zstd`、`segment:xz`；缺省空=不压缩 |
| shard_max_bytes | shard 最大字节数，0=不限制 |
| shard_count | shard 数量 |

#### file

| 属性 | 说明 |
|------|------|
| file_mode | 文件原始权限位 |
| ctime | 创建时间 |
| mtime | 最后修改时间 |

#### shard

| 属性 | 说明 |
|------|------|
| stripe | EC 条带编号（1-based） |
| position | 条带内位置（1-based，1..k 为 DATA，k+1..k+m 为 EC） |
| padded_size | 零填充对齐后的统一大小 |
| original_size | 数据 shard 原始字节数（去 padding 用） |
| data_bytes | 所有 segment 原始字节总和 |

## Shard 定义文件（.def）

### 文件名

```
<4位序号>[.<compress>].<format>.def
```

例：`0000.bin.def`、`0001.zst.bin.def`

### 内容格式

```
# dataset_id: 3
相对路径/a.txt
相对路径/b.txt
```

- `# dataset_id`：由 gen-def 自动写入；`# arcset_id`（可选）：arcset 模式下由 gen-def 写入
- 后续每行一个相对路径（相对于 `dataset.current_path`）
- 一个 shard 不跨多个 dataset
- 也支持 JSON 行：`{"path":"...","offset":0,"size":1024}`，`offset`/`size` 选填

### gen-def：双模式

**1. 内置模式**（缺省）：

```sh
packfs dataset create --root-dir=/data --shard-max-bytes=1073741824
```

`dataset create` 自动扫描目录 → `GenerateShardDefs` 分组 → `MakeShard` 打包，一步到位，不写 .def 文件。

**2. 脚本模式**（自定义分组场景）：

```sh
# 第一步：仅扫描
packfs dataset create --root-dir=/data --gen-only

# 第二步：外部脚本生成 .def 文件
bash ./gen-def.sh --dataset-id=1 --target-root=./data/def

# 第三步：打包
packfs shard make --def-file=./data/def/0000.tar.def --output-dir=./data/shard
```

- 外部脚本自行连接 DB（`SQLITE_DB` 环境变量）、查询 `t_file`、生成 .def 文件
- .def 格式需遵守标准：`# dataset_id` 头 + 路径行 / JSON segment 行
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
dataset:   active ──> archived        (dataset finalize 后)
              └─────> absorbed        (make-ec 后)

arcset:    building ──> complete ──> ready ──> taped
               (validate 全过)  (finalize)  (写入磁带)
```

| 状态 | 实体 | 含义 |
|------|------|------|
| `active` | dataset | 文件在 current_path，可直接访问 |
| `archived` | dataset | 数据已打包到 shard |
| `absorbed` | dataset | 数据已通过 EC 编码写入 arcset，原始 shard 不再独立管理 |
| `building` | arcset | dataset 已关联，shard / EC 正在生成 |
| `complete` | arcset | 所有 shard 完成且校验通过 |
| `ready` | arcset | 已封存：DB 已复制到 current_path，目录自包含 |
| `taped` | arcset | 已写入物理磁带 |

## 纠删码（EC）

基于 Reed-Solomon，将 k 个 data shard 编码为 k+m 个 shard，允许丢失任意 ≤m 个。

**参数约束**：k+m ≤ 255（RS 硬限制）、k ≤ 24（工程上限）、m ∈ {2,4,6}。

**EC 文件命名**：`<stripe>D<position>_<原名>`（数据）/ `<stripe>E<position>.<ext>`（校验）/ `<stripe>D<position>_pad.<ext>`（填充）。

**流程**：
```
1. arcset create --ec=8+4
2. arcset append（关联 dataset，校验 format/compress 兼容性）
3. shard make-ec --arcset-id=1
   └─ PlanStripes → EncodeStripe → 更新 t_shard → dataset → absorbed
```

**恢复**：
```sh
shard recover --arcset-id=1 --shard-file=1D1_0000.tar
```
定位 stripe/position → 收集存活 shard → ReconstructStripe → VerifyStripe。

**重编码**（修改 EC 参数）：
```sh
arcset rebuild --id=1 --ec=12+4
```
删旧 EC/PAD → 重置 status → 重新 make-ec。

### Dataset Finalize 流程

1. 校验所有 shard checksum
2. 确认 `COUNT(t_shard) >= metadata["shard_count"]`
3. 复制 `SQLITE_DB` → `current_path/packfs.db`
4. dataset status → `archived`

封存后 `current_path/` 自包含：`packfs.db` + shard 文件，可直接分发。

### shard_count 检查

`gen-def` 写入 `metadata["shard_count"]`，`validate` 执行比较：

| 实际 vs 预期 | 行为 |
|-------------|------|
| 相等 | OK，状态 → `complete` |
| 实际 > 预期 | warn + 更新 `shard_count` + 状态 → `complete` |
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
GET    /api/health                    → 健康检查
GET    /api/datasets                  → 列表
POST   /api/datasets                  → 创建（扫描+打包）
DELETE /api/datasets/{id}             → 删除（级联 t_file, t_shard, t_segment）
POST   /api/datasets/{id}/finalize    → 归档
GET    /api/datasets/{id}/files       → 文件列表

GET    /api/arcsets                   → 列表
POST   /api/arcsets                   → 创建
POST   /api/arcsets/{id}/append       → 关联 dataset

GET    /api/shards?dataset_id=...     → shard 列表
GET    /api/ec/plan/{arcsetID}        → EC 条带布局
POST   /api/ec/encode/{arcsetID}      → EC 编码
POST   /api/ec/recover/{arcsetID}     → 丢失恢复
```

### 前端

- 纯 HTML/CSS/JS，无框架
- 工作流管道：页面顶部横向展示 `📁 源目录 → 📦 Dataset → 🔧 Shard → 📚 Arcset → 🛡️ EC → 💾 磁带`
- EC 可视化：data shard 大方块 + EC parity 小方块，丢失/恢复状态颜色区分
- CLI 命令栏：实时显示对应 packfs 命令，可一键复制

### 与 CLI 的关系

WebUI 和 CLI 共用 `internal/` 下的 Store 和业务逻辑，数据完全一致。WebUI 上创建、删除、EC 编码等操作等同于执行 CLI 命令。

## 幂等性

`shard make` 使用 `INSERT ... ON CONFLICT(arcset, dataset, file_path) DO UPDATE`，重复运行覆盖原记录（id 不变）。`t_segment` 使用 `DELETE` + 批量 `INSERT`（事务内），确保每次重写的完整替换。
