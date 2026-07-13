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
| status | VARCHAR | `active` / `archived` |
| metadata | JSON | 含 `num_files`、`total_bytes` |
| current_path | VARCHAR | 文件实际存放路径 |
| comment | TEXT | |

### t_arcset

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| name | VARCHAR | |
| label | VARCHAR | |
| status | VARCHAR | `building` / `complete` / `ready` |
| metadata | JSON | 见下方 metadata 定义 |
| current_path | VARCHAR | shard 输出目录 |
| last_check | DATETIME | |
| comment | TEXT | |

### t_shard

| 列 | 类型 | 说明 |
|----|------|------|
| id | INTEGER PK | |
| seq | SMALLINT | 序号 |
| file_path | TEXT | 相对 arcset.current_path 的路径 |
| file_size | BIGINT | |
| type | VARCHAR | DATA / EC |
| checksum | VARCHAR | SHA-256 |
| metadata | JSON | 含 `shard_type` |
| last_check | DATETIME | |
| arcset | INTEGER FK | NOT NULL |
| dataset | INTEGER FK | NOT NULL |

唯一索引：`(arcset, dataset, file_path)`

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
| format | `bin` / `tar` / `iso`，缺省 `bin` |
| compress | 压缩配置，缺省空=不压缩。shard 级省略前缀，segment 级标注 `segment:`。例：`zstd`、`segment:zstd`、`zstd_seekable`、`xz` |
| shard_max_bytes | shard 最大字节数 |
| shard_count | 预期最少 shard 数（`gen-def` 自动写入） |
| tape_max_bytes | 磁带大小（字节数） |
| ec | Erasure Code 参数，例 `8+4` |
| total_bytes | 所有 dataset 原始文件总字节数 |
| sum_bytes | 所有 shard 总字节数 |
| net_bytes | 数据 shard（排除 EC）总字节数 |

#### dataset

| 属性 | 说明 |
|------|------|
| num_files | 文件总数量 |
| total_bytes | 数据集总数据量 |

#### file

| 属性 | 说明 |
|------|------|
| file_mode | 文件原始权限位 |
| ctime | 创建时间 |
| mtime | 最后修改时间 |

#### shard

| 属性 | 说明 |
|------|------|
| shard_type | `DATA` / `EC` |
| data_bytes | 所有 segment 原始字节总和 |

## Shard 定义文件（.def）

### 文件名

```
<4位序号>[.<compress>].<format>.def
```

例：`0000.bin.def`、`0001.zst.bin.def`

### 内容格式

```
# arcset_id: 1
# dataset_id: 3
相对路径/a.txt
相对路径/b.txt
```

- `# arcset_id` / `# dataset_id`：由 `gen-def` 自动写入
- 后续每行一个相对路径（相对于 `dataset.current_path`）
- 一个 shard 不跨多个 dataset
- 也支持 JSON 行：`{"path":"...","offset":0,"size":1024}`，`offset`/`size` 选填

### gen-def：双模式

**1. 内置模式**（缺省，数据保存场景）：

```sh
packfs dataset create --gen-only --target-root=/output --id=1
```

1. 按 dataset 分组
2. 组内文件按路径排序，累加超过 `shard_max_bytes` 时关闭当前 shard
3. 单个文件超过 `shard_max_bytes` 时拆分多段，每段一个 segment。不超时文件保持完整
4. 序号从 0 开始，4 位整数命名
5. 拆分片段在 .def 中用 JSON 格式表示（`{"path":"...","offset":0,"size":1024}`），完整文件用纯路径

**2. 脚本模式**（自定义分组场景）：

```sh
packfs dataset create --gen-only --target-root=/output --id=1 --script=./my-gen.sh
```

gen-def 执行外部脚本：

```
./my-gen.sh --id=1 --target-root=/output
```

- 脚本自行连接 DB（`SQLITE_DB` 环境变量）、查询 `t_file`、生成 .def 文件到 `target-root/`
- .def 格式需遵守标准：`# arcset_id` + `# dataset_id` 头 + 路径行
- 脚本 stdout 输出 shard 数量（纯数字），gen-def 据此写入 `metadata["shard_count"]`
- 脚本退出码 0=成功，非 0=失败

**两种模式最后都执行**：统计 target-root 下的 .def 文件数，写入 `metadata["shard_count"]`。

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
dataset:   active ──> archived
               (arcset finalize 后)

arcset:    building ──> complete ──> ready
               (validate 全过)  (finalize)
```

| 状态 | 实体 | 含义 |
|------|------|------|
| `active` | dataset | 文件在 current_path，可直接访问 |
| `archived` | dataset | 数据已打包到 shard，需通过 arcset mount 访问 |
| `building` | arcset | dataset 已关联，shard 正在生成 |
| `complete` | arcset | 所有 shard 完成且校验通过 |
| `ready` | arcset | 已封存：DB 已复制到 current_path，arcset_id 归一为 1，目录自包含 |

### finalize 流程

1. 校验所有 shard checksum
2. 确认 `COUNT(t_shard) >= metadata["shard_count"]`
3. 复制 `SQLITE_DB` → `current_path/packfs.db`
4. 新 DB：`UPDATE t_arcset SET id=1, current_path='.'`
5. arcset status → `ready`，关联 dataset → `archived`

封存后 `current_path/` 自包含：`packfs.db` + shard 文件，可直接分发、挂载。

### shard_count 检查

`gen-def` 写入 `metadata["shard_count"]`，`validate` 执行比较：

| 实际 vs 预期 | 行为 |
|-------------|------|
| 相等 | OK，状态 → `complete` |
| 实际 > 预期 | warn + 更新 `shard_count` + 状态 → `complete` |
| 实际 < 预期 | 报错拦截 |
| 未设置 | 跳过检查 |

## 幂等性

`shard make` 使用 `INSERT ... ON CONFLICT(arcset, dataset, file_path) DO UPDATE`，重复运行覆盖原记录（id 不变）。`t_segment` 使用 `DELETE` + 批量 `INSERT`（事务内），确保每次重写的完整替换。
