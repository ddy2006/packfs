# packfs CLI

## 代码架构

三层分离：

```
cmd/cli/
├── main.go               # 入口，组装 cobra 命令树
├── root.go               # package main：rootCmd、日志初始化、错误处理
├── dataset/dataset.go    # package dataset：create、list 命令逻辑
├── arcset/arcset.go      # package arcset：make、gen-def、unpack 命令逻辑
├── shard/shard.go        # package shard：make、unpack、make-ec、recover 命令逻辑
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

- 路径参数统一使用 `--source-root` / `--target-root`，表示输入根目录和输出根目录。
- 单文件输入使用具体参数名（如 `--shard-file`、`--def-file`、`--ec-shard-file`）。

## Shard 定义文件（.def）

Shard 定义文件描述一个 shard 包含哪些文件/片段，由 `arcset gen-def` 生成，也支持用户自定义脚本生成。

### 文件名约定

```
<id>[.<compress>].<format>.def
```

| 示例 | id | 压缩 | 打包格式 |
|------|----|------|---------|
| `0001.bin.def` | 0001 | 无 | 二进制拼接 |
| `0002.zst.bin.def` | 0002 | zstd | 二进制拼接 + zstd 压缩 |
| `0003.tar.def` | 0003 | 无 | tar（含元数据） |
| `myarcset.bin.def` | myarcset | 无 | 二进制拼接 |

- **id**：顺序号或标识名，`arcset gen-def` 默认用 arcset 名称
- **compress**：可选，压缩算法（目前 `zst`）
- **format**：打包格式，`bin`（二进制拼接）/ `tar` / `iso`。目前实现 `bin`

### 内容格式

每行一个 segment，两种写法：

```
相对路径/to/file_a.txt
{"path":"相对路径/to/file_b.txt","offset":4096,"size":1024}
```

- 不以 `{` 开头 → 相对路径，完整文件（offset=0, size=整个文件）
- 以 `{` 开头 → JSON 行（不含空格），`path` 必填，`offset`/`size` 选填

### JSON 字段

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `path` | string | 是 | - | 文件相对路径 |
| `offset` | int64 | 否 | 0 | 文件内起始偏移 |
| `size` | int64 | 否 | 0 | 读取字节数，0=直到文件末尾 |

## 命令参考

### arcset

```sh
# 创建归档集
packfs arcset make \
  --source-root=/data/source \
  --target-root=/data/output \
  --name=arcset-001 \
  --dataset-ids=1,2,3

# 生成 shard 定义文件（输出到 target-root/<name>.bin.def）
packfs arcset gen-def \
  --name=arcset-001 \
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
| `gen-def` | `--name` | 是 | arcset 名称 |
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
| | `--def-file` | 是 | shard 定义文件（`.def`，格式见上方说明） |
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
