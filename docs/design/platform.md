# 平台与质量

## 1. PostgreSQL

现状：`build/postgres/` 有 schema，但代码只有 SQLite Store；`go.mod` 无 PG 驱动，三个 Store 接口没有 PG 实现。

目标架构：dataset 层每个 dataset 一个 SQLite；arcset 层使用全局 PostgreSQL。因此 PG 不是可选扩展，而是 arcset 的必需依赖。

需要补：

- PG 驱动（建议 `pgx`）与 `internal/db.OpenPostgres`。
- PG schema：`build/postgres/arcset_schema.sql`，见 [storage-architecture.md](storage-architecture.md)。
- dataset SQLite schema：`build/sqlite/dataset_schema.sql`。
- `PGStore`：arcset、relation、shard、mount 的读写；SQL 方言差异（JSONB、partial index、占位符）。
- dataset SQLite Store：按文件打开，只读模式 `mode=ro` / `immutable=1`。
- `t_fs_mount` 在 PG；不使用本地状态库。
- PG schema 版本化迁移；`build/postgres/arcset_schema.sql` 为初始化脚本，迁移脚本与其保持一致。
- 测试：dataset Store 用内存/临时 SQLite；arcset Store 用 PG 容器（testcontainers 或 docker compose）。

## 2. 测试缺口

- `internal/api`：无测试；补 httptest + 临时 SQLite/PG 的 handler 测试。
- `internal/fuse`：无测试；先测 dataset 挂载索引与 arcset 跨库索引，再做只读挂载集成测试（CI 可能无 FUSE，标记 skip）。
- `internal/db`：无测试；补 dataset SQLite schema、PG schema、迁移测试。
- 端到端：覆盖 dataset create → append（注册/冻结）→ build-data（移动/写 PG 行）→ EC → validate → export → recover。
- 故障注入：模拟 build-data 在文件移动后、PG 行写入前中断，验证幂等重试。
- EC 流式：用小文件做正确性测试；用分块计数/内存上限测试确认不会整 shard 读入（见 [ec-streaming.md](ec-streaming.md)）。
- schema：dataset SQLite 只读/冻结测试；PG schema 迁移测试。

## 3. 文档同步

已确认不一致：

- 根 README：仍写 `arcset gen-def/validate/finalize`、`fs mount --arcset-id`、`make-ec --def-file`。
- `cmd/cli/README.md`：同上，且命令参数与当前实现不符。
- `docs/design.md`：arcset 状态机、PG、ISO fork 等表述需更新。
- `CLAUDE.md`：REST 端点数量 14/15 与实际 17 不符；数据库章节仍写单一 SQLite。

做法：实现阶段统一修订；设计阶段先以 `docs/design/` 为准。

## 4. 自动补全

- 为 `--dataset-db`、`--arcset-id`、`--id`、`--def-file`、`--shard-file` 增加 `ValidArgsFunction` / `RegisterFlagCompletionFunc`。
- dataset 候选从本地注册表或目录扫描；arcset 候选从 PG 查询；失败时静默返回空。
- 低风险，可独立于其他设计先做。

## 5. shard validate 批量化

- 现状只有单文件 `shard validate --shard-file`；dataset/arcset 级校验各自实现。
- 建议增加 `shard validate --dataset-db` / `--arcset-id`，复用 dataset shard 校验和 `arcset validate` 的内部函数，CLI 输出汇总。

## 待决问题

1. PG 的部署、备份、高可用与连接配置。
2. 测试是否进 CI（GitHub Actions）？PG 容器与 FUSE 测试如何处理？
3. 自动补全是否列为 P0？
