# packfs CLI

## 全局约定

- 路径参数统一使用 `--source-root` / `--target-root`，表示输入根目录和输出根目录。
- 单文件输入使用具体参数名（如 `--shard-file`、`--def-file`、`--ec-shard-file`）。

## 命令参考

### arcset

```sh
# 创建归档集
packfs arcset make \
  --source-root=/data/source \
  --target-root=/data/output \
  --name=arcset-001 \
  --dataset-ids=1,2,3

# 生成 shard 定义文件
packfs arcset gen-def \
  --source-root=/data/source \
  --target-root=/data/output

# 解包归档集
packfs arcset unpack \
  --source-root=/data/archives \
  --target-root=/data/extracted \
  [--dataset-id=1] \
  [--dataset-name=<regex>]
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `make` | `--source-root` | 是 | 源数据根目录 |
| | `--target-root` | 是 | 输出根目录 |
| | `--name` | 是 | arcset 名称 |
| | `--dataset-ids` | 是 | 关联的 dataset ID，逗号分隔 |
| `gen-def` | `--source-root` | 是 | 源数据根目录 |
| | `--target-root` | 是 | shard-def 文件输出目录 |
| `unpack` | `--source-root` | 是 | shard 文件所在根目录 |
| | `--target-root` | 是 | 解包输出根目录 |
| | `--dataset-id` | 否 | 按 dataset ID 筛选 |
| | `--dataset-name` | 否 | 按 dataset 名称筛选（支持 regex） |

### shard

```sh
# 打包单个 shard
packfs shard make \
  --source-root=/data/source \
  --target-root=/data/output \
  --def-file=shard_001.bin.def

# 解包单个 shard
packfs shard unpack \
  --shard-file=/data/shard_001.bin \
  --target-root=/data/extracted

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
| `make` | `--source-root` | 是 | 源数据根目录 |
| | `--target-root` | 是 | shard 输出目录 |
| | `--def-file` | 是 | shard 定义文件（每行一个相对路径） |
| `unpack` | `--shard-file` | 是 | 待解包的 shard 文件 |
| | `--target-root` | 是 | 解包输出目录 |
| `make-ec` | `--def-file` | 是 | EC 定义的 YAML 文件 |
| `recover` | `--ec-shard-file` | 是 | 用于恢复的 EC shard 文件 |
| | `--target-root` | 是 | 恢复输出目录 |

### dataset

```sh
# 从目录创建数据集
packfs dataset create \
  --source-root=/data/source \
  --name=my-dataset

# 列出数据集
packfs dataset list \
  [--dataset-id=1] \
  [--dataset-name=<regex>]
```

| 子命令 | 参数 | 必填 | 说明 |
|--------|------|------|------|
| `create` | `--source-root` | 是 | 源数据根目录 |
| | `--name` | 是 | dataset 名称 |
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
