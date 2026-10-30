# packfs

将小文件打包成大文件（shard），减少磁盘碎片，提高性能。

```
dataset SQLite ──dataset_uid──> PG t_dataset_ref ──r_arcset_dataset_ref──> arcset
       └── DATA shard ──> segment ──> file
```

## 快速开始

```sh
# 1) 创建 dataset（独立 SQLite）
packfs dataset create --root-dir=/data/source --db-dir=/data/datasets \
  --target-root=/data/shards --gen-only

# 2) 生成 .def（外部脚本，头写 # dataset_db）
bash gen-def.sh --dataset-db=/data/datasets/<uid>.db --target-root=/data/def

# 3) 打包 DATA shard
packfs shard make --def-file=/data/def/0000.tar.def --output-dir=/data/shards

# 4) 标记 dataset built
packfs dataset mark-built --dataset-db=/data/datasets/<uid>.db

# 5) 创建 arcset（PG）
packfs arcset create --name=my-arc --target-root=/data/arc \
  --ec=8+4 --partition-capacity=18TB

# 6) 注册 dataset、布局、EC、校验、导出
packfs arcset append --id=1 --dataset-db=/data/datasets/<uid>.db
packfs arcset build-data --id=1
packfs arcset make-ec --id=1
packfs arcset validate --id=1 --stage=all
packfs arcset export --id=1

# 7) 恢复
packfs arcset recover --id=1 --shard-file=00/000D-ds3f2a1c9b-0000.tar
```

环境变量：

- `PACKFS_PG_URL`：arcset 全局 PostgreSQL DSN。
- `PACKFS_DATASET_DB_DIR`：dataset SQLite 缺省目录。
- `PACKFS_TEST_PG_URL`：仅测试用；PG 集成测试在临时 schema 中运行，未设置时跳过。

完整应用验收见 `examples/astro/`；WebUI 通过 `packfs serve --db-dir=...` 启动，arcset 功能需要 `PACKFS_PG_URL`。

## 打包格式

支持 **bin**（二进制拼接）、**tar** 和 **iso**，以及 **zstd** / **xz** / **zstd_seekable** 压缩（shard 级或 segment 级）。

## 文档

| 文档 | 内容 |
|------|------|
| `docs/design/README.md` | 概要设计、实现状态、已确认方向 |
| `docs/design/storage-architecture.md` | dataset SQLite + PostgreSQL |
| `docs/design/commands-db.md` | 命令、DB 字段与实现顺序 |
| `docs/design/ec-streaming.md` | 大 shard 流式 EC |
| `examples/astro/README.md` | 应用级全流程验收 |

## 技术栈

Go + SQLite + PostgreSQL + Cobra + Reed-Solomon。详见 `CLAUDE.md`。
