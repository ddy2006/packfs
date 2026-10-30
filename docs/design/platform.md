# 平台与质量

## 1. PostgreSQL

现状（2026-10-30）：核心链路与 API/WebUI 已切到两层存储。`pgx`、`PGStore`、per-dataset SQLite Store、共享 datasetops/arcsetops、流式 EC、FUSE 索引、API/WebUI 和 examples 已落地。

仍需补：

- PG schema 版本化迁移；`build/postgres/arcset_schema.sql` 为初始化脚本，`internal/db/postgres_schema.sql` 为嵌入式测试副本，两者必须同步。
- PG 集成测试已补基础（临时 schema）；仍缺故障注入与高并发测试。
- API/WebUI 已切到新 Store；旧命令 `dataset validate/finalize`、`arcset rebuild`、旧 `shard make-ec/recover` 已移除。
- `arcset show/expand` 与 `--ec-parallel` 已实现；expand/create 有测试，show 与并行编码仍缺专门断言。
- `t_fs_mount` 在 PG；`fs umount`/`fs list` 已实现，`fsck` 未实现。

## 2. 测试缺口

- `internal/api`：已有 dataset 无 PG 冒烟测试、arcset + PG 全流程 httptest 测试。
- `internal/arcset`：PG Store 生命周期测试（create/append/idempotency/shards/mounts）。
- `internal/dataset` / `datasetops`：DatasetStore 生命周期、create/BuildShards/MarkBuilt/Scan。
- `internal/arcsetops`：create/expand/ParseBytes；append 由 builddata/FUSE 测试覆盖。
- `internal/ec`：多 chunk 的流式 encode/verify/reconstruct 正确性；尚无硬性内存上限断言和 100GB 基准。
- `internal/builddata`：placement 单元测试；PG 下的幂等、删除 PG 行后的崩溃重试测试。
- `internal/fuse`：dataset 索引/DirTree、arcset 跨库索引测试；实际 mount 在本机 macFUSE 阻塞。
- `internal/db`：仍缺 dataset SQLite schema、PG schema、迁移测试。
- 故障注入：已覆盖 build-data 单行缺失重试；仍缺 PG 写入中途失败、进程中断等更强故障注入。
- WebUI：无浏览器自动化；API 层已覆盖。

## 3. 文档同步

已同步：

- 根 `README.md`、`examples/astro/README.md`、`CLAUDE.md`、`internal/CLAUDE.md`、`internal/ec/CLAUDE.md`、`cmd/cli/CLAUDE.md` 已按新 CLI/两层存储更新。
- `docs/design.md` 的 EC 流程已改为 `arcset make-ec` / `arcset recover`。

仍待同步：

- `cmd/cli/README.md` 仍是旧命令参考。
- `webui/README.md` 已按新 API/下游存储描述更新。
- `docs/todo.md` 的 Phase 进度尚未重排。

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
