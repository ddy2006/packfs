# packfs

将小文件打包成大文件（shard），减少磁盘碎片，提高性能。

```
dataset ──> arcset ──> shard ──> segment ──> file
```

## 快速开始

```sh
# 1) 创建 dataset
packfs dataset create --root-dir=/data/source --name=my-ds

# 2) 创建 arcset
packfs arcset create --name=my-arc --target-root=/data/output --dataset-ids=1

# 3) 生成 shard 定义文件
packfs arcset gen-def --id=1 --target-root=/data/output

# 4) 打包 shard
packfs shard make --def-file=/data/output/0000.bin.def

# 5) 校验
packfs arcset validate --id=1

# 6) 封存
packfs arcset finalize --id=1

# 7) 解包
packfs shard unpack --shard-file=/data/output/0000.bin --target-root=/extract
```

## 打包格式

支持 **bin**（二进制拼接）和 **tar** 两种打包格式，以及 **zstd** / **xz** / **zstd_seekable** 压缩（shard 级或 segment 级）。

![ER 图](packfs-erd.svg)

## 文档

| 文档 | 内容 |
|------|------|
| `cmd/cli/README.md` | 完整命令参考 |
| `docs/design.md` | 数据库设计、状态机、metadata 定义、分包逻辑 |
| `docs/todo.md` | 计划中的功能 |

## 技术栈

Go + SQLite + Cobra。详见 `CLAUDE.md`。
