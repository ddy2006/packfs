# TODO

## 实施计划进度

| Phase | 内容 | 状态 |
|-------|------|------|
| **Phase 1** | 数据库结构调整（t_shard 双 nullable FK + CHECK + 条件唯一索引） | ✅ 完成 |
| **Phase 2** | dataset 命令实现（create 合并 gen-def、unpack/validate/finalize 从 arcset 迁移） | ✅ 完成 |
| **Phase 3** | arcset 命令简化（删除已迁移子命令，简化 create，新增 append/list） | ✅ 完成 |
| **Phase 4** | 新功能实现（make-ec、recover、rebuild） | ✅ 完成 |
| **Phase 5** | WebUI 管理面板（packfs serve，REST API，真实后端驱动） | ✅ 完成 |
| **Phase 6** | 仿真数据生成集成到 WebUI | ✅ 完成 |
| **Phase 7** | 文件系统层（只读 FUSE 挂载、bin 格式） | ✅ 完成 |

---

## ✅ Phase 1-4 已完成

### dataset 命令
- dataset create（扫描目录 + 写入 format/compress/shard_max_bytes 存储配置）
  - `--gen-only`：仅扫描，跳过 shard 生成，配合自定义 gen-def 脚本使用
- dataset list / unpack / validate / finalize
  - 均支持 `--source-root` 指定 shard 文件目录（默认 dataset.current_path）
- dataset 状态机：active → archived（finalize 后）/ active → absorbed（make-ec 后）
- **NEW** Store 接口新增 `Delete`：级联删除 t_segment → t_shard → r_arcset_dataset → t_file → t_dataset

### arcset 命令
- arcset create（--name/--target-root/--ec/--tape-max-bytes/--tape-count）
- arcset append（配置兼容性校验 + 关联，首次 append 继承 shard_max_bytes）
- arcset list / rebuild

### shard 命令
- shard make / make-ec / recover / unpack / validate
- EC 编码：PlanStripes → EncodeStripe → 更新 t_shard → dataset → absorbed
- EC 恢复：定位 stripe/position → 收集存活 shard → ReconstructStripe → VerifyStripe
- 幂等：shard make ON CONFLICT DO UPDATE

### 示例
- examples/astro：完整演示流程
  - simulate.sh（生成仿真数据）→ full-pipeline.sh（完整打包+EC 流程）
  - 数据集：`dataset.def` 配置，24 channel × 80 时间片 = 1920 文件
- examples/astro/scalebox：分布式打包

---

## ✅ Phase 5: WebUI 管理面板（已完成）

### packfs serve 命令
- `packfs serve --addr=:8080` 启动内嵌 HTTP server
- WebUI 编译进二进制（`//go:embed webui`），零外部依赖
- 和 CLI 共用 `internal/` Store 层，数据完全一致

### REST API
| 方法 | 路由 | 功能 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET/POST | `/api/datasets` | 列表 / 创建（扫描+打包） |
| DELETE | `/api/datasets/{id}` | 删除（级联清理） |
| POST | `/api/datasets/{id}/finalize` | 归档 |
| GET | `/api/datasets/{id}/files` | 文件列表 |
| GET/POST | `/api/arcsets` | 列表 / 创建 |
| POST | `/api/arcsets/{id}/append` | 关联 dataset |
| GET | `/api/shards?dataset_id=...` | shard 列表 |
| GET | `/api/ec/plan/{arcsetID}` | EC 条带布局 |
| POST | `/api/ec/encode/{arcsetID}` | EC 编码 |
| POST | `/api/ec/recover/{arcsetID}` | 丢失恢复 |

### 前端特性
- 工作流管道：`📁 源目录 → 📦 Dataset → 🔧 Shard → 📚 Arcset → 🛡️ EC → 💾 磁带`，自动跟踪进度
- EC 可视化：data shard 大方块 + EC parity 小方块，丢失（红色脉冲）→ 恢复（绿色）
- CLI 命令栏：实时显示等效 packfs 命令，可一键复制
- 纯 HTML/CSS/JS，无前端框架依赖

### 新增文件
```
internal/api/handler.go          # REST handler 层，14 个端点
cmd/cli/webui/serve.go           # packfs serve 命令
embed.go                         # //go:embed webui 静态文件
webui/js/app.js                  # 前端（fetch API，async 数据刷新）
```

---

## ✅ Phase 6: 仿真数据生成集成到 WebUI（已完成）

将 `examples/astro/simulate.sh` 的仿真数据生成功能搬进 WebUI。

### 实现方式：Go 内实现（方案 1）

- `internal/simulate/simulate.go`：纯 Go 实现，`crypto/rand` 生成随机数据文件
- `POST /api/simulate`：接收 Config JSON，返回 `{file_count, total_bytes, output_dir}`
- 文件命名与 `simulate.sh` 一致：`{ts}_{next_ts}_ch{ch}.dat`

### WebUI 页签

- 🧪 仿真数据 页签：参数表单 + "📋 SKA 预设" 一键填入默认天体物理数据集配置
- 生成完成后显示结果卡片，提供 "📦 用此目录创建 Dataset" 快捷按钮
- 切换回 Dataset 页签后自动填入源目录和名称

### 新增/修改文件
```
internal/simulate/simulate.go         # 仿真数据生成核心（新）
internal/api/handler.go               # +simulateData handler + 路由
webui/index.html                      # +仿真数据页签
webui/js/app.js                       # +仿真生成前端逻辑
```

---

## ✅ Phase 7: 文件系统层（已完成 — bin 格式只读 FUSE 挂载）

### 实现

- `internal/fuse/fuse.go`：基于 `hanwen/go-fuse/v2` 的只读 FUSE 文件系统，`fs.InodeEmbedder` + `OnAdd` 构建虚拟目录树
- `internal/fuse/index.go`：从 DB（`t_file JOIN t_segment JOIN t_shard`）构建内存索引，file_path → `[]SegmentLoc{ShardPath, Offset, Size, Csize}`
- `cmd/cli/fs/fs.go`：`packfs fs mount --dataset-id=1 --mount-point=/mnt/pk`

### 支持

- bin 格式，无压缩：直接 seek+read，零拷贝开销
- bin 格式，segment:zstd/segment:xz：按 segment 解压后返回请求字节
- bin 格式，zstd/xz（shard 级）：全 shard 解压（慢，仅作 fallback）

### 验证

- `ls -laR` 正确展示目录树和文件大小
- `cmp` 逐字节一致
- `dd skip=N` 随机读取位置也一致

### 新增/修改文件
```
internal/fuse/fuse.go                # FUSE 文件系统（新）
internal/fuse/index.go               # DB → 内存索引构建（新）
cmd/cli/fs/fs.go                     # packfs fs mount 命令（重写）
```

---

## 待实现（其他）

### 灵活的 gen-def 格式
- 按文件数量分组
- 定制的排序方式
- shard 文件名的定制模板

### PostgreSQL 支持
- 为三个 Store 接口实现 `store_postgres.go`
- 数据库导入/导出

### 性能优化
- 打包性能优化（scalebox 分布式并行打包）
- 解包性能优化

### 功能特性
- 子命令的 auto completion
- zstd seekable 格式完善
- ISO 固定时间戳（`internal/iso9660/` fork 完善）
