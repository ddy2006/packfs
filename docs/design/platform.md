# 平台与质量

## 1. PostgreSQL

现状（2026-10-29）：核心链路已切到两层存储。`pgx`、`PGStore`、per-dataset SQLite Store、流式 EC、FUSE 索引和 examples 已落地；API/WebUI 仍是旧单 SQLite 路径。

仍需补：

- PG schema 版本化迁移；`build/postgres/arcset_schema.sql` 为初始化脚本，`internal/db/postgres_schema.sql` 为嵌入式测试副本，两者必须同步。
- PG 集成测试、故障注入测试、build-data 中断重试测试。
- API/WebUI 切到新 Store；旧命令 `dataset validate/finalize`、`arcset rebuild`、旧 `shard make-ec/recover` 清理。
- `arcset show/expand` 与 `--ec-parallel`。
- `t_fs_mount` 在 PG；`fs umount`/`fs list` 已实现，`fsck` 未实现。

## 2. 测试缺口

- `internal/api`：无测试；补 httptest + 临时 SQLite/PG 的 handler 测试。
- `internal/fuse`：无测试；先测 dataset 挂载索引与 arcset 跨库索引，再做只读挂载集成测试（CI 可能无 FUSE，标记 skip）。
- `internal/db`：无测试；补 dataset SQLite schema、PG schema、迁移测试。
- 端到端：覆盖 dataset create → append（注册/冻结）→ build-data（移动/写 PG 行）→ EC → validate → export → recover。
- 故障注入：模拟 build-data 在文件移动后、PG 行写入前中断，验证幂等重试。
- EC 流式：用小文件做正确性测试；用分块计数/内存上限测试确认不会整 shard 读入（见 [ec-streaming.md](ec-streaming.md)）。
- schema：dataset SQLite 只读/冻结测试；PG schema 迁移测试。

## 3. 文档同步

已同步：

- 根 `README.md`、`examples/astro/README.md`、`CLAUDE.md`、`internal/CLAUDE.md`、`internal/ec/CLAUDE.md`、`cmd/cli/CLAUDE.md` 已按新 CLI/两层存储更新。
- `docs/design.md` 的 EC 流程已改为 `arcset make-ec` / `arcset recover`。

仍待同步：

- `cmd/cli/README.md` 仍是旧命令参考。
- `webui/README.md` 与 WebUI 文案仍按旧 API/介质状态描述。
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
