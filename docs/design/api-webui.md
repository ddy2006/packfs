# API / WebUI 缺口

## 现状（2026-10-29）

- CLI 核心链路已切到新两层存储；`internal/api` 仍是旧单 SQLite 实现，`packfs serve` 尚未迁移。
- 旧 17 个路由（`internal/api/handler.go`）假设 `s.DB`；新 API 需要改为 PG + dataset SQLite 组合。
- `POST /api/shards/validate/{id}` 仍返回 501。
- WebUI “解包”按钮只提示用 CLI（`webui/js/app.js:444`）。
- 缺少 arcset 的 append/build/validate/make-ec/recover/export、dataset/shard unpack、fs 管理、shard 按 ID 查询。
- 数据库将拆为 dataset SQLite + 全局 PG；现有 API 假设单一 SQLite。
- WebUI 管道末端原“介质”步骤依赖 ready/taped；packfs 不感知下游介质后应改为“移交下游存储”，只显示 `status=completed`。

## 设计要点

### 补齐端点

| 方法 | 路由 | 说明 |
|------|------|------|
| POST | `/api/datasets` | 创建 dataset SQLite；请求含 `db_dir` / `target_root`，返回 `dataset_uid` / `sqlite_path` |
| GET | `/api/datasets/{uid}` | 读取 dataset SQLite 元数据 |
| GET | `/api/shards/{uid}/{seq}` | 单 DATA shard 详情 |
| POST | `/api/shards/{uid}/{seq}/validate` | 校验单个 DATA shard |
| POST | `/api/shards/{uid}/{seq}/unpack` | 解包到指定目录 |
| POST | `/api/datasets/{uid}/unpack` | 解包整个 dataset |
| POST | `/api/arcsets/{id}/append` | 注册 dataset SQLite、分配 dataset_key/append_order、冻结 SQLite；构建期间可多次调用（lazy） |
| POST | `/api/arcsets/{id}/expand` | 增加 `group_count`，扩大容量 |
| POST | `/api/arcsets/{id}/build-data` | 读 dataset SQLite，移动文件并写 PG `t_shard` |
| POST | `/api/arcsets/{id}/validate` | 按阶段校验：data / ec / all |
| POST | `/api/arcsets/{id}/make-ec` | 触发 EC 生成/重算；packfs 暂按单机同步执行，流水线调度由外部系统负责 |
| POST | `/api/arcsets/{id}/recover` | `target=data/all`，按组恢复；checksum 不变；`target=data` 置 `parity_degraded=true` |
| POST | `/api/arcsets/{id}/export` | 生成 arcset.json；不改变 status，可重复执行 |
| POST | `/api/arcsets/{id}/rebuild` | 改 EC/分组/规格后重建；首版不实现 |
| DELETE | `/api/arcsets/{id}` | 删除 arcset（级联规则待定） |
| GET | `/api/fs/mounts` | PG `t_fs_mount` 列表 |
| POST | `/api/fs/mount` | 挂载 dataset（`--dataset-db`）或 arcset（二选一）；arcset 根目录为 `<dataset_key>/` |
| POST | `/api/fs/unmount` | 卸载 |

### WebUI

- 管道图：`定义 → 数据 shard → EC → 校验 → 🤝 移交下游存储`；移除原“💾 介质/磁带”步骤。
- Arcset 页签：显示 `status` / `build_phase` / `health` / `parity_degraded`、dataset relation（`dataset_uid` / `dataset_key`）、`group_count`、`layout`、`seq_width`、每个 partition 的 `used/available_capacity`、shard 完成度；提供 append、build、expand、validate、make-ec、recover、export、delete 操作；rebuild 首版不实现。
- `status=building, phase=ec` 期间的 append 返回 busy；packfs 暂不展示外部流水线的任务进度。
- Dataset/Shard 页签：显示 `dataset_uid` / `sqlite_path` / `building|built` / 是否冻结；解包按钮接真实 API。
- 新增 FUSE 页签：挂载列表、挂载/卸载操作；dataset 挂载只对未吸收 dataset 显示。
- 不做介质/磁带页签；相关操作属于下游存储系统。

### 约定

- 长操作（build/validate/make-ec/recover/export）暂按同步或 CLI 驱动；任务化/异步 API 留给外部流水线集成，后续再设计。
- packfs 不假设 EC 构建由单个进程完成，也不在本次 API 中实现流水线调度。
- unpack 目标目录必须校验路径（禁止越界）；默认不覆盖已有文件。
- API 与 CLI 共用 internal 层，不复制业务逻辑。
- dataset 操作直接打开对应 SQLite；arcset 操作访问 PG。

## 待决问题

1. arcset delete 的级联范围：只删 PG/目录，还是同时删除 dataset SQLite？
2. 解包是否允许覆盖？默认目标目录规则？
3. WebUI 是否需要触发下游移交（调用外部 API），还是仅生成 manifest 由人工导入？
4. 长操作是否需要异步任务/轮询，还是同步阻塞？
5. `build-data` 是独立端点，还是复用 `POST /api/shards` 批量创建？
6. WebUI/API 是否允许直接访问 PG，还是只通过 packfs 服务代理？
