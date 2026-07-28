# TODO

## ✅ 已实现

### dataset 命令
- dataset create（扫描目录 + 写入 format/compress/shard_max_bytes 存储配置）
  - `--gen-only`：仅扫描，跳过 shard 生成，配合自定义 gen-def 脚本使用
- dataset list / unpack / validate / finalize
  - 均支持 `--source-root` 指定 shard 文件目录（默认 dataset.current_path）
- dataset 状态机：active → archived（finalize 后）

### arcset 命令
- arcset create（--name/--target-root/--ec/--tape-max-bytes/--tape-count，不再持有存储配置）
- arcset append --id=<arcset> --dataset-id=<ds>（配置兼容性校验 + 关联，首次 append 继承 shard_max_bytes）
- arcset list [--id=<arcset>]（列出所有 arcset 或 arcset 中的 dataset）

### shard 命令
- shard make（支持 `--output-dir` 覆盖输出目录；def 文件可无 arcset_id，自动从 dataset metadata 读配置）
- shard make-ec --arcset-id（EC 编码：PlanStripes → EncodeStripe → 更新 t_shard → dataset absorbed）
- shard recover --arcset-id --shard-file（从 EC stripe 恢复丢失 shard）
- shard unpack / validate

### 数据模型
- t_shard：双 nullable FK（arcset / dataset）+ CHECK 约束 + 条件唯一索引
- SegmentDesc / ShardDef 归属 internal/dataset（从 internal/arcset 迁移）
- GenerateShardDefs：单 dataset 版本（internal/dataset/gen_def.go），纯函数，按 shard_max_bytes 分组

### 存储与格式
- shard 定义文件（.def）：`# dataset_id` 头 + 路径行 / JSON segment 行
- 分包：按 dataset 粒度，shard_max_bytes 控制
- 压缩：zstd / xz / zstd_seekable（shard 级）/ segment:zstd / segment:xz
- 幂等：shard make ON CONFLICT DO UPDATE
- 文件大小校验：shard make 对比 DB 与实际大小，不一致 warning

### 示例
- examples/astro：重构为 dataset 命令流程
  - dataset create --gen-only → gen-def.sh → shard make --output-dir → dataset validate/finalize --source-root
- examples/astro/scalebox：分布式打包（待完整实现）

## 实施计划进度

| Phase | 内容 | 状态 |
|-------|------|------|
| **Phase 1** | 数据库结构调整（t_shard 双 nullable FK + CHECK + 条件唯一索引） | ✅ 完成 |
| **Phase 2** | dataset 命令实现（create 合并 gen-def、unpack/validate/finalize 从 arcset 迁移） | ✅ 完成 |
| **Phase 3** | arcset 命令简化（删除已迁移子命令，简化 create，新增 append/list） | ✅ 完成 |
| **Phase 4** | 新功能实现（make-ec、recover、rebuild） | ✅ 完成 |

### Phase 3 已完成

- [x] `arcset create` 移除 `--dataset-ids`、`--format`、`--compress`、`--shard-max-bytes`
- [x] `arcset create` 新增 `--tape-max-bytes`、`--tape-count`
- [x] 新增 `arcset append --id=<arcset> --dataset-id=<ds>`（配置兼容性校验 + 关联 dataset + 首次 append 继承 shard_max_bytes）
- [x] 新增 `arcset list [--id=<arcset>]`（列出所有 arcset 或指定 arcset 中的 dataset）

### Phase 4 已完成

- [x] `shard make-ec --arcset-id=<id>`：查 arcset → 收集 data shard → ec.PlanStripes → ec.EncodeStripe → 更新/写入 t_shard → dataset → absorbed
- [x] `shard recover --arcset-id=<id> --shard-file=<path>`：定位丢失 shard 的 stripe/position → 找同 stripe 存活 shard → ec.ReconstructStripe → ec.VerifyStripe
- [x] `arcset rebuild --id=<id> [--ec=<k+m>] [--tape-count=<n>]`：更新 EC 参数 → 删旧 EC/PAD → 调 shard.MakeECShard 重新编码
- [x] E2E 测试通过：create → append → make-ec(2+2) → rebuild(4+2) → recover（修 3 个 bug）

## 待实现

### 灵活的 gen-def 格式

- 按文件数量分组
- 定制的排序方式
- shard文件名的定制模板
- 纠删码的实现

示例：

dataset 1177938016：
- 文件名：`1177938016/1177938016_1177940019_ch148.dat`，
  - 其中ch部分从133到156，共计24个channel
  - 1177940019表示时间，从1177940019到1177944816，共4798秒
- 数据文件总数：115152个

shard分组方法
- 按channel分组
- 每组40个，40秒一个shard，末尾可能不完全对齐
- 分组后shard文件名：1177938016/1177940019_1177940058_ch148.tar.zst

外部 gen-def 脚本（examples/astro/gen-def.sh）已实现上述分组逻辑，通过独立脚本调用而非 dataset create 内置。

### 性能优化

- 打包性能优化（scalebox 分布式并行打包）
- 解包性能优化

## 磁带特性

### arcset存储参数选择
- 单磁带shard数 n=磁带字节数/shard字节数
- k+m ：k个数据块 + m个校验块，交错分布在k+m个
- 数据shard数量：k * n 个
- EC shard数量：m * n 个

- 参数值限制
  - k + m <= 255 ，算法底层硬限制
  - 单组数据块：k <= 24，人为设定的工程上限，超过上限，对硬件要求高
  - m = 2, 冗余最低，成本最优
  - m = 4，容错能力强，综合性价比高
  - m = 6，仅特殊场合使用

### 纠删码的shard文件名命名规则
文件名：类型码(组编号)-(组内顺序号).(格式).(压缩)
其中：
- 类型码：D:data，E:ec码
- 组编号：0..k+m-1
- 组内顺序号：0..n-1
- 格式：bin/tar/iso
- 压缩：zst/xz

逻辑顺序号：按组内顺序号、组编号的顺序来进行，均匀分布在各个磁带存储单元中

### 纠删码编码实现(k+m)

- 算法：
  - 标准RS(Reed-Solomon)算法（MDS纠删码），k个数据块，m个校验块
  - 标识方法：RS(8,4)

## 文件系统

### 文件系统特性
- 只读文件系统
  - ls
  - open/read
  - ？
- 支持部分读取
  - shard不存在，可以ls，但是不能read

### 文件系统性能测试工具集
### 文件系统读取性能测试
### 文件系统读取性能优化

## zstd seekable

## iso打包格式

- iso9660 库（`github.com/kdomanski/iso9660`）`WriteTo()` 内部硬编码 `time.Now()`，写入 PVD 字段，导致每次生成的 ISO 镜像字节不同。
  - 已 fork 到 `internal/iso9660/`，`ImageWriter` 增加 `Timestamp` 字段，固定时间戳。

## postgresql支持

- 已有代码的所有数据库操作，支持sqlite/postgresql
- 数据库导入/导出支持

## 功能特性

- 子命令的auto completion
