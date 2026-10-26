# 数据库 schema

新设计采用两层数据库：

| 文件 | 数据库 | 用途 |
|------|--------|------|
| `sqlite/dataset_schema.sql` | 每个 dataset 一个 SQLite | `t_dataset` / `t_file` / `t_shard`(DATA) / `t_segment`；只读 |
| `postgres/arcset_schema.sql` | 全局 PostgreSQL | `t_dataset_ref` / `t_arcset` / `r_arcset_dataset_ref` / `t_shard`(DATA/EC/PAD) / `t_fs_mount` |

- dataset SQLite 首次 arcset 注册后冻结；不含 arcset 表；SQLite 只读。
- PostgreSQL 不保存 file/segment 明细；DATA shard 的 `file_size` / `sha256` 是快照；`t_fs_mount` 也在 PG。
- 无跨库外键；逻辑键为 `dataset_uid` + `seq`。
- `postgres/Dockerfile` 直接初始化 `postgres/arcset_schema.sql`。
- `initdb.d/20-erd.sql`、`initdb.d/21-updated.sql`、`sqlite/erd.sql` 是旧单库 schema，迁移完成后删除或替换。
