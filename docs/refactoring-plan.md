# packfs 架构重构方案

## 概述

将 dataset 与 arcset 的职责重新划分：

- **dataset**：磁盘数据归档单元。持有存储配置（format / compress / shard_max_bytes），独立管理 data shard 的生成、校验、解包、封存。
- **arcset**：支持纠删码冗余的 dataset 容器。管理磁带布局、EC 保护、跨 dataset 的 shard 编目。不持有存储配置（除 EC 参数外）。

---

## 1. 数据库结构调整

### 1.1 t_shard 表

```sql
CREATE TABLE t_shard (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seq INTEGER,
  file_path TEXT,
  file_size BIGINT,
  type VARCHAR,          -- DATA / EC / PAD
  metadata JSON,
  sha256 VARCHAR,
  last_check DATETIME,
  arcset INTEGER REFERENCES t_arcset(id),   -- nullable
  dataset INTEGER REFERENCES t_dataset(id),  -- nullable
  CHECK (dataset IS NOT NULL OR arcset IS NOT NULL)
);

-- 条件唯一索引（替换原 (arcset, dataset, file_path) 联合唯一索引）
CREATE UNIQUE INDEX idx_t_shard__dataset_file_path
  ON t_shard (dataset, file_path) WHERE dataset IS NOT NULL;
CREATE UNIQUE INDEX idx_t_shard__arcset_file_path
  ON t_shard (arcset, file_path) WHERE arcset IS NOT NULL;
```

**约束**：shard 不能同时为孤儿（至少归属一方）：
- `dataset IS NOT NULL AND arcset IS NULL` → dataset 自己的 data shard
- `dataset IS NOT NULL AND arcset IS NOT NULL` → arcset 中的 data shard（同时关联来源 dataset）
- `dataset IS NULL AND arcset IS NOT NULL` → arcset 的 EC / PAD shard（无 dataset 来源）
- `dataset IS NULL AND arcset IS NULL` → 禁止（孤儿 shard）

### 1.2 三种 shard 的归属

| shard 类型 | arcset FK | dataset FK | file_path 示例 | 说明 |
|-----------|-----------|------------|---------------|------|
| dataset data shard | NULL | Y | `0000.bin` | dataset.create 生成，独立属于 dataset |
| arcset data shard | X | Y | `0000D01_0000.bin` | make-ec 后重命名，同时关联 arcset 和来源 dataset |
| arcset EC shard | X | NULL | `0000E04.bin` | make-ec 生成，仅属于 arcset |
| arcset PAD shard | X | NULL | `0001P01.bin` | EC stripe 补齐，仅属于 arcset |

### 1.3 metadata 字段迁移

| 字段 | 迁移方向 | 说明 |
|------|---------|------|
| `format` | arcset.metadata → dataset.metadata | 打包格式（bin / tar / iso） |
| `compress` | arcset.metadata → dataset.metadata | 压缩配置 |
| `shard_max_bytes` | arcset.metadata + dataset.metadata 各存一份 | dataset 用于 gen-def 分片；arcset 用于 append 时的兼容性校验 |
| `ec` | 保留在 arcset.metadata | EC 参数（k+m），arcset 层概念 |
| `shard_count` | 各自管理 | dataset 管 data shard 数；arcset 管总 shard 数 |

### 1.4 arcset.metadata 新增字段

| 字段 | 缺省值 | 说明 |
|------|--------|------|
| `ec` | — | EC 参数（如 `"8+4"`） |
| `shard_max_bytes` | — | 规范分片大小。append 时校验 dataset 的值必须与此一致 |
| `tape_max_bytes` | — | 单盘磁带容量（字节） |
| `tape_count` | — | 磁带总数（= k+m） |
| `seq_width` | 4 | seq（顺序号）在文件名中的固定宽度 |
| `group_width` | 2 | group（组号）在文件名中的固定宽度 |

---

## 2. CLI 命令重新分配

### 重构对照表

```
重构前                              重构后
──────────────────────────────────────────────────────────────
dataset create                      dataset create
  (纯扫描目录)                         (扫描目录 + gen-def)
                                    dataset create --gen-only
                                      (仅生成 .def，不做 shard make)
dataset list                        dataset list

                                    dataset unpack         ← 从 arcset 迁移
                                    dataset validate       ← 从 arcset 迁移
                                    dataset finalize       ← 从 arcset 迁移

arcset create                       arcset create           ← 简化
  (含 --dataset-ids, --format,        (只含 --ec, --tape-*)
   --compress, --shard-max-bytes)
arcset gen-def                      [删除]
arcset unpack                       [删除]
arcset validate                     [删除]
arcset finalize                     [删除]
                                    arcset append           ← 新增
                                    arcset rebuild          ← 新增
                                    arcset list             ← 新增

shard make                          shard make              ← 适配（读 dataset metadata）
shard make-ec                       shard make-ec           ← 不变（待完整实现）
shard recover                       shard recover           ← 不变（待完整实现）
shard unpack                        shard unpack            ← 不变
shard validate                      shard validate          ← 不变
```

### 2.1 dataset 命令详情

#### dataset create

```
packfs dataset create --root-dir=/data --name=ds1
  [--format=bin] [--compress=zstd] [--shard-max-bytes=1G]
  [--gen-only] [--target-root=/output]
```

流程：
1. 扫描目录 → `t_dataset` + `t_file`
2. gen-def（内置分组逻辑）→ 生成 `.def` 文件
3. 如果 `--gen-only`：仅生成 .def，输出到 `--target-root`，结束
4. 否则：遍历 .def → `shard make` → `t_shard`（dataset FK = 当前 ID，arcset FK = NULL）
5. 回写 `metadata["shard_count"]`

参数说明：
- `--format`：打包格式，缺省 `bin`。值：`bin` / `tar` / `iso`，写入 `dataset.metadata["format"]`
- `--compress`：压缩配置，缺省空（不压缩）。写入 `dataset.metadata["compress"]`
- `--shard-max-bytes`：shard 最大字节数。写入 `dataset.metadata["shard_max_bytes"]`
- `--gen-only`：仅生成 .def 文件，不做 shard make
- `--target-root`：.def 输出目录（仅 --gen-only 时需要）
- `--root-dir`：数据源目录
- `--name`：dataset 名称

#### dataset unpack

```
packfs dataset unpack --id=1 --target-root=/out
  [--name=ds1]
```

流程：
1. 查找 dataset 的所有 data shard（`t_shard WHERE dataset=X AND type='DATA'`）
2. 逐个解包到 `--target-root`

#### dataset validate

```
packfs dataset validate --id=1
```

流程：
1. 查找 dataset 的所有 data shard
2. 逐个重算 SHA-256 checksum，与 DB 记录对比
3. 输出 OK/FAIL 结果

#### dataset finalize

```
packfs dataset finalize --id=1
```

流程：
1. 校验所有 data shard checksum
2. 复制 SQLite DB → `dataset.current_path/packfs.db`
3. dataset 状态 → `archived`

### 2.2 arcset 命令详情

#### arcset create

```
packfs arcset create --name=a1 --target-root=/tapes
  [--ec=8+4] [--tape-max-bytes=10G] [--tape-count=8]
```

参数说明：
- `--name`：arcset 名称
- `--target-root`：输出目录（tape 挂载点或聚合目录）
- `--ec`：EC 参数（k+m）
- `--tape-max-bytes`：单盘磁带容量
- `--tape-count`：磁带总数

不再包含的旧参数：`--dataset-ids`、`--format`、`--compress`、`--shard-max-bytes`。

#### arcset append

```
packfs arcset append --id=1 --dataset-id=2
```

流程：
1. 校验 dataset 与 arcset 的配置一致性：
   - `shard_max_bytes` 必须相等
   - `format` 必须相同
   - `compress` 必须相同
   - 不一致时拒绝 append，提示先执行 `arcset rebuild` 统一配置
2. 在 `r_arcset_dataset` 中创建关联
3. 为 dataset 的 data shard 分配 seq 编号（在 arcset 已有最大 seq + 1 之后顺排）
4. 后续执行 `shard make-ec` 完成 EC 编码和文件重命名

#### arcset rebuild

```
packfs arcset rebuild --id=1 [--ec=8+4] [--tape-count=8]
```

修改 arcset 的 EC 参数或磁带配置后，重新 EC 编码。第一版仅支持 EC 参数变更（data shard 本身的打包不变）。

#### arcset list

```
packfs arcset list --id=1    # 列出 arcset 中的 dataset
packfs arcset list           # 列出所有 arcset
```

### 2.3 shard 命令（适配）

#### shard make

适配变化：当前从 `arcset.Metadata` 读取 format/compress → 改为从 `dataset.Metadata` 读取（通过 def 文件中的 `dataset_id`）。

#### shard make-ec

待完整实现。核心流程：
1. 读取 arcset 关联的所有 data shard（通过 `r_arcset_dataset` JOIN `t_shard WHERE dataset IS NOT NULL`）
2. 按 append 顺序排列 shard，分配 stripe 编号
3. 调用 `ec.PlanStripes` + `ec.EncodeStripe`
4. 重命名 data shard 文件为 EC 命名格式
5. `t_shard` 记录：data shard 设 `arcset FK → X`（保留 `dataset FK`）；EC / PAD shard 写入（`arcset FK = X, dataset FK = NULL`）

---

## 3. 状态机

### 3.1 Dataset 状态

```
                    ┌─────────┐
                    │  active  │  dataset create 后的初始状态
                    └────┬─────┘
                         │
              ┌──────────┼──────────┐
              │ finalize │          │ make-ec（归属 arcset 后）
              ▼          │          ▼
        ┌──────────┐    │    ┌──────────┐
        │ archived │    │    │ absorbed │
        └──────────┘    │    └──────────┘
              │          │          │
              │ finalize │          │
              ▼          │          │
        (archived 后     │          │
         也可被 arcset    │          │
         append，进入     │          │
         absorbed)       │          │
              └──────────┼──────────┘
                         ▼
                   ┌──────────┐
                   │ absorbed │
                   └──────────┘
```

| 状态 | 触发条件 | 实体 shard 文件 | DB 元数据 | 可执行操作 |
|------|---------|----------------|----------|-----------|
| **active** | `dataset create` 完成 | 存在于 `current_path`，以 dataset 命名（如 `0000.bin`） | 完整（dataset + shard + file + segment） | unpack, validate, finalize |
| **archived** | `dataset finalize` | 存在于 `current_path`，目录自包含（含 `packfs.db`） | 完整，DB 副本在 `current_path` 内 | unpack, validate |
| **absorbed** | `shard make-ec` 完成 | 已重命名为 arcset 命名格式，位于 arcset 目录。dataset 的 `current_path` 下无 shard 文件 | **保留** dataset + shard 级元数据。file / segment 级是否保留待定（PostgreSQL 大库场景下可考虑清理以节省空间） | 通过 arcset 间接访问 |

**状态转换说明：**

- `active → archived`：dataset 自包含封存，不依赖外部 DB。shard 文件保留在本地。
- `active → absorbed`：dataset 被 arcset append 后，经过 `make-ec`，shard 文件被重命名并逻辑上归属 arcset。dataset 自身仅保留元数据。
- `archived → absorbed`：已封存的 dataset 之后也可以被 arcset append，同样进入 absorbed 状态。

**absorbed 状态的元数据保留策略（待定）：**

- SQLite 场景：dataset 自己的 `packfs.db` 保留完整元数据。
- PostgreSQL 大库场景：集中式 DB 中保留 dataset 记录 + t_shard 记录（dataset FK 仍有效）。`t_file` / `t_segment` 级别的记录可考虑清理（不再需要独立解包），但保留后可支持跨 dataset 的文件溯源。**具体清理粒度后续再定。**

### 3.2 Arcset 状态

Arcset 是**长期迭代构建**的容器，生命周期内会多次 append dataset。中间结果始终驻留在磁盘上，最终迁移到物理磁带库。

```
                        append + make-ec（反复）
   ┌──────────┐  ┌─────────────────────────────┐
   │          │  │  append ds1 → make-ec        │
   │ building │──│  append ds2 → make-ec        │   finalize    ┌───────┐   migrate   ┌───────┐
   │ (on disk)│  │  append ds3 → make-ec  ...   │──────────────>│ ready │───────────>│ taped │
   │          │──│  (中间结果始终在磁盘)           │               │(locked)│            │(on tape)│
   └──────────┘  └─────────────────────────────┘               └───────┘            └───────┘
        │                                                           │                    │
        │ rebuild（修改 EC 参数 / 磁带布局）                           │                    │ 本地副本
        └───────────────────────────────────────────────────────────┘                    │ 可清理
                                                                                         │
                                                                                   recover 按需
                                                                                   从磁带回读
```

| 状态 | 含义 | shard 位置 | 可执行操作 |
|------|------|-----------|-----------|
| **building** | 迭代构建中，可随时追加 dataset。每次 append 后执行 make-ec 生成 EC/PAD shard，中间结果始终在磁盘。允许未满（shard 数可能不是 k 的整数倍，末 stripe 由 PAD 补齐）。 | 磁盘（arcset 目录下，EC 命名格式） | append, make-ec, validate, rebuild, list |
| **ready** | `finalize` 后锁定，不再接受新 dataset。全部 EC 完成，全部校验通过，作为磁盘常驻归档等待迁移到磁带。 | 磁盘 | validate, list, migrate to tape |
| **taped** | 已迁移到物理磁带库。shard 元数据保留在 DB，本地磁盘副本按策略清理。 | 磁带（本地可能有缓存副本） | list, recover（按需从磁带回读） |

**迭代构建模式：**

- building 是 arcset 最长的状态。每次 `arcset append --dataset-id=X` 后增量执行 `make-ec`，仅处理新加入的 data shard，已有 EC stripe 不动。
- 中间状态允许 shard 总数不被 k 整除 —— 末 stripe 由 PAD shard 补齐，后续 append 新 dataset 时，PAD shard 可被真实 data shard 替换（或追加到新 stripe）。
- 每次增量 make-ec 后均可执行 validate，无需等 arcset 完整。

**状态转换规则：**

| 转换 | 条件 | 说明 |
|------|------|------|
| `building → ready` | `finalize`：所有 stripe 完整（PAD 已补齐），全部 shard 校验通过 | 锁定 arcset，拒绝后续 append |
| `ready → taped` | 磁带迁移完成 | `t_shard.file_path` 可能更新为磁带路径 |
| `ready → building` | `rebuild`：修改 EC 参数或磁带布局 | 允许从 ready 回退，方便调整参数后重新 make-ec |
| `taped → building` | 不允许 | 已迁移到磁带后不支持回退；如需修改，创建新 arcset |

**与 dataset 的联动：**

- arcset append 时，dataset 仍在 `active` 或 `archived` 状态。
- arcset make-ec 完成后，被吸收的 dataset 进入 `absorbed` 状态（见 3.1 节）。
- arcset finalize 不影响 dataset 状态（dataset 已经是 absorbed）。
- arcset taped 后，dataset 保持 absorbed。

### 3.3 共享磁盘目录布局

Arcset 运行在共享磁盘（NAS / 并行文件系统）上，shard 文件和所有关联 dataset 的 `packfs.db` 均存放在同一目录树下。迁移到磁带时，整个目录树作为完整的元数据+数据集合被写入磁带。

```
<arcset_current_path>/                  # 共享磁盘上的 arcset 根目录
├── packfs.db                           # arcset 自身 DB（finalize 后生成）
├── datasets/                           # 关联 dataset 的元数据目录
│   ├── <ds_id_1>/                      # dataset 1
│   │   └── packfs.db                   #   dataset 1 的元数据库（归档副本）
│   ├── <ds_id_2>/
│   │   └── packfs.db                   #   dataset 2 的元数据库
│   └── ...
├── 0000D00_0000.bin                    # EC data shard（dataset 1 的第 1 个 shard）
├── 0000D01_0001.bin
├── 0000E02.bin                         # EC parity shard
├── 0001D00_0002.bin                    # EC data shard（dataset 2 的第 1 个 shard）
├── 0001D01_0003.bin
├── 0001E02.bin
└── ...
```

**设计要点：**

- **自包含**：arcset 目录包含恢复所需的全部信息 —— shard 数据 + arcset 元数据 + 每个 dataset 的元数据。即使中心 PostgreSQL 大库不可用，也能从磁带恢复后重建。
- **dataset packfs.db 的来源**：
  - `dataset finalize` 时已在 `dataset.current_path/packfs.db` 生成了一份。
  - `arcset append` 时，将 dataset 的 `packfs.db` 复制（或硬链接）到 `<arcset>/datasets/<ds_id>/packfs.db`。
  - 如果 dataset 未 finalize 就被 append，arcset 从中心 DB 导出 dataset 元数据生成 `packfs.db`。
- **共享磁盘**：arcset 目录必须在共享存储上（NAS / Lustre / GPFS），确保多节点可访问 shard 文件和 dataset 元数据。单个 dataset 的原始 `current_path` 可以是本地路径，但被 arcset 吸收后以 arcset 目录下的副本为准。

---

## 4. Arcset 的 Shard 命名规则

Arcset 中的 shard 文件名同时编码了 EC 纠删码布局和磁带布局：

```
<seq:0W>S<type><group:0W>_<原始dataset shard名>   → data shard
<seq:0W>S<type><group:0W>.<ext>                   → EC / PAD shard

W = seq_width（缺省 4）或 group_width（缺省 2），不足位补 0。
```

**字段说明：**

| 字段 | 宽度 | 缺省值 | 说明 |
|------|------|--------|------|
| seq | seq_width | 4 | 顺序号，0-based，磁带内的排序编号 |
| type | 1 | — | shard 类型：`D`（data）、`E`（EC）、`P`（PAD） |
| group | group_width | 2 | 组号，0-based，磁带号。0..k-1 = 数据磁带，k..k+m-1 = EC 校验磁带 |

**编号规则：**

- seq / group 均为 0-based
- append dataset 时，新 shard 的 seq 从 arcset 已有最大 seq + 1 开始，保证每条磁带上编号连续
- `seq_width` 和 `group_width` 在 `arcset.metadata` 中配置，可覆盖缺省值

**示例 1**（k=3, m=2, arcset 含 2 个 dataset，各有 6 和 3 个 data shard，均整除 k，无 PAD）：

```
Stripe 0: 0000D00_0000.bin, 0000D01_0001.bin, 0000D02_0002.bin → 0000E03.bin, 0000E04.bin   (dataset A)
Stripe 1: 0001D00_0003.bin, 0001D01_0004.bin, 0001D02_0005.bin → 0001E03.bin, 0001E04.bin   (dataset A)
Stripe 2: 0002D00_0000.bin, 0002D01_0001.bin, 0002D02_0002.bin → 0002E03.bin, 0002E04.bin   (dataset B)
```

每条磁带上存放同 group 的所有 shard：
- Tape 0：0000D00_0000.bin, 0001D00_0003.bin, 0002D00_0000.bin
- Tape 1：0000D01_0001.bin, 0001D01_0004.bin, 0002D01_0001.bin
- Tape 2：0000D02_0002.bin, 0001D02_0005.bin, 0002D02_0002.bin
- Tape 3：0000E03.bin, 0001E03.bin, 0002E03.bin
- Tape 4：0000E04.bin, 0001E04.bin, 0002E04.bin

**示例 2**（k=3, m=2, arcset 含 1 个 dataset，4 个 data shard，末 stripe 需 2 个 PAD）：

```
Stripe 0: 0000D00_0000.bin, 0000D01_0001.bin, 0000D02_0002.bin → 0000E03.bin, 0000E04.bin   (dataset A)
Stripe 1: 0001D00_0003.bin, 0001P01.bin, 0001P02.bin           → 0001E03.bin, 0001E04.bin   (2 PAD)
```

### 与 EC 模块的映射

EC 模块内部使用 stripe/position 术语，与 arcset 的 seq/group 对应：

| arcset | EC 模块 | 关系 |
|--------|---------|------|
| seq | stripe | seq = stripe（数值相同） |
| group | position | group = position（数值相同） |
| seq_width | — | EC 模块不处理宽度格式化，由调用方负责 |
| group_width | — | 同上 |

宽度格式化在 arcset 层完成：EC 模块生成 StripeFile 结构体后，调用方按 `seq_width` / `group_width` 格式化为最终文件名。

---

## 5. 实施计划

### Phase 1：数据库结构调整

- [ ] 修改 `internal/db/schema.sql`：t_shard FK nullable + CHECK + 条件唯一索引
- [ ] 同步更新 `build/sqlite/erd.sql` 和 `build/postgres/initdb.d/20-erd.sql`
- [ ] 更新 `internal/shard/shard.go`：Shard struct 适配 nullable FK
- [ ] 更新 `internal/shard/store_sqlite.go`：查询适配 nullable
- [ ] 更新 `internal/shard/store.go`：Store 接口新增 `FindByDataset` 方法

### Phase 2：dataset 命令实现

- [ ] `dataset create` 合并 gen-def 逻辑（`--gen-only` 参数控制）
- [ ] 将 `GenerateShardDefs` 从 `internal/arcset/segment.go` 移至 `internal/dataset/`（或独立分组包）
- [ ] `dataset create` 新增 `--format`、`--compress`、`--shard-max-bytes` 参数
- [ ] `dataset unpack`（从 arcset unpack 迁移）
- [ ] `dataset validate`（从 arcset validate 迁移）
- [ ] `dataset finalize`（从 arcset finalize 迁移）
- [ ] `dataset list`（已有，保持不变）

### Phase 3：arcset 命令简化

- [ ] `arcset create` 移除 `--dataset-ids`、`--format`、`--compress`、`--shard-max-bytes`
- [ ] `arcset create` 新增 `--tape-max-bytes`、`--tape-count`
- [ ] 新增 `arcset append`（配置兼容性校验 + 关联 dataset）
- [ ] 新增 `arcset list`（列出 arcset 中的 dataset）
- [ ] 删除 `arcset gen-def`、`arcset unpack`、`arcset validate`、`arcset finalize`

### Phase 4：新功能实现（后续展开）

- [ ] `shard make-ec` 完整实现
- [ ] `shard recover` 完整实现
- [ ] `arcset rebuild` 实现
- [ ] EC 前缀命名与磁带布局的完整集成

### 验证

- Phase 2 完成后，用 `examples/astro` 的完整流程验证 dataset 命令：
  ```
  packfs dataset create --root-dir=... --format=bin
  packfs shard make --def-file=...
  packfs dataset validate --id=...
  packfs dataset finalize --id=...
  packfs dataset unpack --id=... --target-root=/tmp/restore
  ```

---

## 6. 关键设计决策汇总

| 决策 | 结论 |
|------|------|
| t_shard FK 约束 | 双 nullable + XOR CHECK + 条件唯一索引 |
| 存储配置归属 | format / compress / shard_max_bytes → dataset；ec → arcset |
| shard 命名 | `<seq:0W><type><group:0W>_<name>`，W 可配置（缺省 seq=4, group=2） |
| seq / group 起始 | 0-based |
| seq_width | 缺省 4，可配置 |
| group_width | 缺省 2，可配置 |
| append 编号 | seq 在 arcset 已有最大 seq + 1 顺排 |
| 向后兼容 | 不考虑 |
| dataset finalize | 需要，独立于 arcset |
| arcset finalize | 暂不实现，后续再定 |
| 前缀方案 | 无 DS 前缀，直接用 EC 命名格式编码磁带布局 |
