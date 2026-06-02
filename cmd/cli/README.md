# packfs CLI

## 代码架构

三层分离：

```
cmd/cli/
├── main.go               # 入口，组装 cobra 命令树
├── root.go               # package main：rootCmd、日志初始化、错误处理
├── dataset/dataset.go    # package dataset：create、list 命令逻辑
├── arcset/arcset.go      # package arcset：create、gen-def、unpack、validate、finalize 命令逻辑
├── shard/shard.go        # package shard：make、unpack、validate、make-ec、recover 命令逻辑
├── fs/fs.go              # package fs：mount、umount、fsck 命令逻辑
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

Shard 定义文件描述一个 shard 包含哪些文件/片段，由 `arcset gen-def` 生成，也支持用户自定义脚本生成。

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
# arcset_id: 1
# dataset_id: 3
相对路径/to/file_a.txt
相对路径/to/file_b.txt
```

- `# arcset_id: N`：arcset 标识，`shard make` 据此获取输出目录（`t_arcset.current_path`）。
- `# dataset_id: N`：数据集标识，`shard make` 据此获取源目录（`t_dataset.current_path`）。
- 后续每行一个相对路径（相对于 `dataset.current_path`），完整文件。
- 一个 shard 不会跨多个 dataset。

### JSON 字段

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `path` | string | 是 | - | 文件相对路径 |
| `offset` | int64 | 否 | 0 | 文件内起始偏移 |
| `size` | int64 | 否 | 0 | 读取字节数，0=直到文件末尾 |

## 命令参考

### arcset

```sh
# 创建归档集（target-root 写入 t_arcset.current_path）
packfs arcset create \
  --target-root=/data/output \
  --name=arcset-001 \
  --dataset-ids=1,2,3

# 生成 shard 定义文件（内置模式）
packfs arcset gen-def --id=1 --target-root=/data/output

# 脚本模式（自定义分组）
packfs arcset gen-def --id=1 --target-root=/data/output --script=./my-gen.sh

# 校验所有 shard checksum
packfs arcset validate --id=1

# 封存 arcset（校验 → 复制 DB → 归一 ID → 设状态）
packfs arcset finalize --id=1

# 解包归档集
packfs arcset unpack \
  --source-root=/data/archives \
  --target-root=/data/extracted \
  [--dataset-id=1] \
  [--dataset-name=<regex>]
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `create` | `--target-root` | 是 | 输出根目录（写入 current_path） |
| | `--name` | 是 | arcset 名称 |
| | `--dataset-ids` | 是 | 关联的 dataset ID，逗号分隔 |
| | `--format` | 否 | 打包格式 bin/iso/tar（缺省 bin） |
| | `--shard-max-bytes` | 否 | shard 最大字节数 |
| | `--compress` | 否 | 压缩配置：`zstd`、`segment:zstd`、`zstd_seekable`、`segment:xz`、`xz` |
| `gen-def` | `--id` | 是 | arcset ID |
| | `--target-root` | 是 | shard-def 文件输出目录 |
| | `--script` | 否 | 外部脚本路径（接收 --id 和 --target-root） |
| `unpack` | `--source-root` | 是 | shard 文件所在根目录 |
| | `--target-root` | 是 | 解包输出根目录 |
| | `--dataset-id` | 否 | 按 dataset ID 筛选 |
| | `--dataset-name` | 否 | 按 dataset 名称筛选（支持 regex） |
| `validate` | `--id` | 是 | arcset ID |
| `finalize` | `--id` | 是 | arcset ID |

### shard

```sh
# 打包单个 shard（源路径和目标路径从 DB 自动获取）
packfs shard make \
  --def-file=/absolute/path/to/0000.bin.def

# 解包单个 shard（tar 格式自带边界，不需要 --arcset-id）
packfs shard unpack \
  --shard-file=/data/0000.tar.zst \
  [--target-root=/data/extracted]

# 解包 bin 格式 shard（需要 --arcset-id 定位文件边界）
packfs shard unpack \
  --shard-file=/data/shard_001.bin \
  --target-root=/data/extracted \
  --arcset-id=1

# 生成纠删码 shard
packfs shard make-ec \
  --def-file=ec_config.yaml

# 从 EC shard 恢复数据
packfs shard recover \
  --ec-shard-file=/data/shard_001.ec \
  --target-root=/data/recovered
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `make` | `--def-file` | 是 | shard 定义文件绝对路径（含 `# arcset_id`） |
| `unpack` | `--shard-file` | 是 | 待解包的 shard 文件 |
| | `--target-root` | 否 | 解包输出目录（缺省当前目录） |
| | `--arcset-id` | 否 | arcset ID，bin 格式必填，tar 格式不需要 |
| `make-ec` | `--def-file` | 是 | EC 定义的 YAML 文件 |
| `recover` | `--ec-shard-file` | 是 | 用于恢复的 EC shard 文件 |
| | `--target-root` | 是 | 恢复输出目录 |

### dataset

```sh
# 从目录递归创建数据集
packfs dataset create \
  --root-dir=/data/source \
  [--name=my-dataset]

# 列出数据集
packfs dataset list \
  [--dataset-id=1] \
  [--dataset-name=<regex>]
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `create` | `--root-dir` | 是 | 源根目录（递归扫描） |
| | `--name` | 否 | dataset 名称，默认 root-dir 最后一级 |
| `list` | `--dataset-id` | 否 | 按 ID 筛选 |
| | `--dataset-name` | 否 | 按名称筛选（支持 regex） |

### fs

```sh
# 挂载 arcset 为文件系统
packfs fs mount \
  --mount-point=/mnt/packfs \
  --arcset-id=1

# 卸载文件系统
packfs fs umount \
  --mount-point=/mnt/packfs

# 文件系统完整性检查
packfs fs fsck \
  --arcset-id=1
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `mount` | `--mount-point` | 是 | 挂载点目录 |
| | `--arcset-id` | 是 | 要挂载的 arcset ID |
| `umount` | `--mount-point` | 是 | 挂载点目录 |
| `fsck` | `--arcset-id` | 是 | 待检查的 arcset ID |
