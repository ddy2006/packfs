# API / WebUI

## 现状（2026-10-30）

- `internal/api` 已切到 dataset SQLite + PostgreSQL；`packfs serve --db-dir=...` 启动。
- dataset 端点扫描 `--db-dir` 下的独立 SQLite；arcset 端点使用 `PACKFS_PG_URL`。
- 未配置 PG 时 dataset 功能可用，arcset 端点返回 503。
- WebUI 管道末端已改为“移交下游存储”，不再展示介质/磁带状态。
- 长操作（build-data / make-ec / validate / recover / export）仍为同步调用。

### 已实现端点

| 方法 | 路由 | 说明 |
|------|------|------|
| GET | `/api/health` | 进程、PG、dataset 目录状态 |
| GET | `/api/datasets` | 扫描 dataset 目录 |
| POST | `/api/datasets` | 创建 dataset SQLite；可选直接生成 shard |
| GET | `/api/datasets/{uid}` | dataset 详情 |
| GET | `/api/datasets/{uid}/files` | 文件清单 |
| GET | `/api/datasets/{uid}/shards` | DATA shard + segment |
| POST | `/api/datasets/{uid}/mark-built` | 校验 shard 并置 `built` |
| POST | `/api/datasets/{uid}/make-shards` | 为 `--gen-only` dataset 生成 shard |
| POST | `/api/datasets/{uid}/unpack` | 解包整个 dataset |
| GET | `/api/shards?dataset_uid=...` | dataset shard 列表 |
| GET | `/api/shards?arcset_id=...` | PG shard 列表 |
| GET / POST | `/api/arcsets` | arcset 列表 / 创建 |
| GET | `/api/arcsets/{id}` | arcset 详情 + dataset relation |
| POST | `/api/arcsets/{id}/append` | 注册 dataset、冻结 SQLite |
| POST | `/api/arcsets/{id}/build-data` | 排序并放置 DATA shard |
| POST | `/api/arcsets/{id}/make-ec` | 生成/重算 EC + PAD |
| POST | `/api/arcsets/{id}/validate` | `stage=data|ec|all` |
| POST | `/api/arcsets/{id}/expand` | 增加 `group_count` |
| POST | `/api/arcsets/{id}/recover` | 恢复丢失 shard |
| POST | `/api/arcsets/{id}/export` | 生成 `arcset.json` |
| GET | `/api/arcsets/{id}/shards` | PG shard 列表 |
| GET | `/api/arcsets/{id}/plan` | 实际条带布局（WebUI EC 预览） |
| GET | `/api/fs/mounts` | PG `t_fs_mount` 列表 |
| DELETE | `/api/fs/mounts?mount_point=...` | 删除挂载记录 |
| POST | `/api/simulate` | 生成仿真数据 |

WebUI 旧路径 `/api/ec/plan/{id}`、`/api/ec/encode/{id}`、`/api/ec/recover/{id}` 保留为兼容别名。

### 尚未实现

- 单 shard 详情 / 单 shard 校验 / 单 shard 解包端点。
- dataset / arcset 删除端点（级联范围待定）。
- `arcset rebuild`（首版不实现）。
- FUSE 挂载/卸载 API 与 WebUI 页签；目前只提供挂载记录列表/删除，挂载仍用 CLI。
- 异步任务/轮询；长操作同步阻塞。

## WebUI

- 管道图：`源目录 → Dataset → Shard → Arcset → EC → 🤝 移交下游存储`。
- Dataset 页签：UID、独立 SQLite、`building|built`、文件数/shard 数；支持创建、Mark Built、查看 shard、解包。
- Shard 页签：按 dataset UID 查看 DATA shard 与 segment；`--gen-only` dataset 可生成 shards。
- Arcset 页签：创建（EC/groups/layout/partition-capacity）、append dataset、列表显示 `status/build_phase/health/parity_degraded`、容量。
- EC 页签：按实际 PG 布局展示 data/EC/PAD；支持 build-data、make-ec、validate、export、expand、recover。
- 不展示介质/磁带状态；也不展示外部流水线任务进度。

## 约定

- API 与 CLI 共用 `datasetops` / `arcsetops` / `builddata` / `makeec` / `validate` / `recoverec` / `export`，不复制业务逻辑。
- dataset 操作直接打开对应 SQLite；arcset 操作访问 PG。
- API 使用 serve 的单一 `--db-dir` 定位 dataset；请求体不再接受 `db_dir`，避免 UID 与目录失配。
- 解包路径经 `safeJoin` 校验，禁止绝对路径和 `..` 越界。
- 长操作暂按同步执行；任务化留给外部流水线。

## 待决问题

1. arcset delete 的级联范围：只删 PG/目录，还是同时删除 dataset SQLite？
2. 解包是否允许覆盖？默认目标目录规则？
3. WebUI 是否需要触发下游移交（调用外部 API），还是仅生成 manifest 由人工导入？
4. 长操作是否需要异步任务/轮询，还是同步阻塞？
5. FUSE 挂载是否由 serve 进程托管，还是始终由 CLI 触发？
