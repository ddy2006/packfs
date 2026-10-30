# packfs CLI

## 代码架构

三层分离：

```
cmd/cli/
├── main.go               # 入口，组装 cobra 命令树
├── root.go               # package main：rootCmd、日志初始化、错误处理
├── dataset/dataset.go    # package dataset：create、list、mark-built、unpack
├── arcset/arcset.go      # package arcset：create、append、build-data、make-ec、validate、recover、export、list
├── shard/shard.go        # package shard：make、unpack、validate
├── fs/fs.go              # package fs：mount、umount、list、fsck
└── README.md
```

```
cmd/cli/<domain>/   →  CLI 适配层：参数解析、调用 internal、格式化输出
internal/<domain>/  →  领域模型 + Store 接口 + 业务逻辑
```

- 每个子包导出 `Command() *cobra.Command`，自行管理 flag 和子命令注册。
- `main.go` 仅做命令树组装，不包含业务逻辑。
- CLI 适配层可脱离 cobra 独立测试。

## 全局约定

- 路径参数使用 `--source-root` / `--target-root` 表示输入/输出根目录。部分命令自动从 DB 获取这些路径，无需显式传参。
- 单文件输入使用具体参数名（如 `--shard-file`、`--def-file`、`--ec-shard-file`）。
- `--def-file` 应使用绝对路径。

## Shard 定义文件（.def）

Shard 定义文件描述一个 shard 包含哪些文件/片段，由外部脚本（如 `examples/astro/gen-def.sh`）生成；`shard make` 消费。

### 分包逻辑

- 单个文件超过 `shard_max_bytes` 时拆分多段；不超时文件保持完整。
- 文件按顺序累加写入当前 shard 的 .def。
- 当加入下一个文件（或片段）会使当前 shard 总字节数超过 `shard_max_bytes` 时，关闭当前 shard，创建下一个 shard。
- 未设置 `shard_max_bytes` 时，所有文件归入一个 shard。
- shard 文件名用 4 位整数序号：`0000.bin.def`、`0001.bin.def`……
- 拆分片段在 .def 中用 JSON 格式表示，完整文件用纯路径。

### 文件名约定

```
<4位序号>.<格式描述>.def
```

| 示例 | compress | 说明 |
|------|----------|------|
| `0000.bin.def` | 无 | 二进制拼接，不压缩 |
| `0000.tar.def` | 无 | tar 归档，不压缩 |
| `0001.bin.zst.def` | `zstd`/`xz` | 先拼 .bin 再整体压缩（shard 级） |
| `0001.tar.zst.def` | `zstd`/`xz` | tar 归档后再整体压缩（shard 级） |
| `0002.zst.bin.def` | `segment:zstd`/`segment:xz` | 每段先压缩，再拼接（segment 级） |
| `0002.zst.tar.def` | `segment:zstd`/`segment:xz` | 每段先压缩，再写入 tar（segment 级） |

### 内容格式

首行为元数据注释，后续每行一个 segment：

```
# dataset_db: /data/datasets/<uid>.db
# dataset_id: 3
相对路径/to/file_a.txt
相对路径/to/file_b.txt
```

- `# dataset_db: <path>`：`shard make` 打开该 dataset SQLite，读取源目录、format/compress 与输出目录。
- `# dataset_id: N`：可选；dataset SQLite 内通常只有一行，缺省取第一行。
- `# arcset_id: N`：旧路径；仅在没有 `# dataset_db` 时使用，新流程不再生成。
- 后续每行一个相对路径（相对于 `t_dataset.current_path`），完整文件。
- 一个 shard 不会跨多个 dataset。

### JSON 字段

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `path` | string | 是 | - | 文件相对路径 |
| `offset` | int64 | 否 | 0 | 文件内起始偏移 |
| `size` | int64 | 否 | 0 | 读取字节数，0=直到文件末尾 |

## 命令参考

环境变量：

- `PACKFS_PG_URL`：arcset 元数据 PostgreSQL DSN。
- `PACKFS_DATASET_DB_DIR`：dataset SQLite 缺省目录，未设置时 `~/data/packfs/datasets`。

### dataset

```sh
# 扫描源目录并直接生成 DATA shard
packfs dataset create \
  --root-dir=/data/source \
  --db-dir=/data/datasets \
  --target-root=/data/shards \
  [--name=astro] [--format=bin] [--compress=zstd] [--shard-max-bytes=1073741824]

# 只创建 SQLite，DATA shard 由外部 shard make 生成
packfs dataset create ... --gen-only
packfs dataset mark-built --dataset-db=/data/datasets/<uid>.db

# 列出目录中的 dataset
packfs dataset list --db-dir=/data/datasets [--name=astro] [--status=built]

# 从 DATA shard 还原文件
packfs dataset unpack \
  --dataset-db=/data/datasets/<uid>.db \
  --target-root=/data/restore \
  [--source-root=/data/shards]
```

| 子命令 | 主要参数 |
|--------|----------|
| `create` | `--root-dir`、`--db-dir`、`--target-root`、`--name`、`--format`、`--compress`、`--shard-max-bytes`、`--shard-sort-regex`、`--shard-sort-index`、`--gen-only` |
| `mark-built` | `--dataset-db` |
| `list` | `--db-dir`、`--name`、`--status` |
| `unpack` | `--dataset-db`、`--target-root`、`--source-root` |

### arcset

```sh
# 定义 arcset（元数据写 PostgreSQL）
packfs arcset create \
  --name=arc1 --target-root=/data/arc1 \
  --ec=8+4 --groups=1 --layout=raid3 \
  --partition-capacity=18TB

# 注册 dataset、布局、EC、校验、导出
packfs arcset append --id=1 --dataset-db=/data/datasets/<uid>.db
packfs arcset build-data --id=1
packfs arcset make-ec --id=1 [--ec-chunk-size=8MiB] [--ec-parallel=1] [--group=0] [--seq=0]
packfs arcset validate --id=1 --stage=all
packfs arcset export --id=1 [--target-root=/data/export]

# 扩容、查看与恢复
packfs arcset expand --id=1 --add-groups=2
packfs arcset list [--id=1] [--status=completed]
packfs arcset show --id=1
packfs arcset recover --id=1 --shard-file=00/000D-ds3f2a1c9b-0000.tar [--target=all]
```

| 子命令 | 主要参数 |
|--------|----------|
| `create` | `--name`、`--target-root`、`--ec`、`--groups`、`--layout`、`--partition-width`、`--seq-width`、`--partition-capacity`、`--shard-max-bytes` |
| `append` | `--id`、`--dataset-db`、`--dataset-key` |
| `build-data` | `--id`、`--dataset-uid` |
| `make-ec` | `--id`、`--ec-chunk-size`、`--ec-parallel`、`--group`、`--seq` |
| `validate` | `--id`、`--stage=data|ec|all` |
| `expand` | `--id`、`--add-groups` |
| `recover` | `--id`、`--shard-file`、`--target=data|all`、`--ec-chunk-size` |
| `export` | `--id`、`--target-root` |
| `list` | `--id`（列出该 arcset 的 dataset）、`--status` |
| `show` | `--id` |

### shard

```sh
# 打包单个 shard；.def 头部含 # dataset_db
packfs shard make --def-file=/absolute/path/to/0000.bin.def [--output-dir=...]

# 解包单个 shard（tar/iso 自带边界；bin 需要 --arcset-id，旧路径）
packfs shard unpack --shard-file=/data/0000.tar.zst [--target-root=/data/extracted]
packfs shard unpack --shard-file=/data/0000.bin --arcset-id=1 --target-root=/data/extracted

# 校验 shard checksum；bin 的 --arcset-id 仍依赖旧全局库
packfs shard validate --shard-file=/data/0000.bin --arcset-id=1
```

### fs

```sh
# dataset 挂载（仅未吸收的 dataset）
packfs fs mount --dataset-db=/data/datasets/<uid>.db --mount-point=/mnt/pk

# arcset 挂载（根目录为 <dataset_key>/）
packfs fs mount --arcset-id=1 --mount-point=/mnt/pk

packfs fs list
packfs fs umount --mount-point=/mnt/pk
```

`fs fsck` 仍是占位实现。

### serve

```sh
packfs serve --addr=:8080 --db-dir=/data/packfs/datasets
```

- WebUI 与 REST API 编译在二进制内。
- dataset 端点扫描 `--db-dir` 下的独立 SQLite；缺省 `$PACKFS_DATASET_DB_DIR` 或 `~/data/packfs/datasets`。
- arcset 端点使用 `PACKFS_PG_URL`；未配置时 dataset 功能仍可用，arcset 返回 503。
