# TODO

## ✅ 已实现

- dataset create / list
- arcset create / gen-def / unpack / validate / finalize
- shard make / unpack / validate（bin 需 --arcset-id）
- shard 定义文件（.def）：纯路径 + JSON segment
- 分包：按 dataset 粒度，shard_max_bytes 控制
- 压缩：zstd / xz / zstd_seekable（shard 级缺省）
- 状态机：building → complete → ready（arcset）、active → archived（dataset）
- finalize：校验 → 复制 DB → arcset_id 归一 → 目录自包含
- 幂等：shard make ON CONFLICT DO UPDATE
- 文件大小校验：shard make 对比 DB 与实际大小，不一致 warning

## 待实现

### 灵活的 gen-def 格式

- 按文件数量分组
- 定制的排序方式
- shard文件名的定制模板
- 纠删码的实现

示例：

dataset 1177938016：
- 文件名：```1177938016/1177938016_1177940019_ch148.dat```，
  - 其中ch部分从133到156，共计24个channel
  - 1177940019表示时间，从1177940019到1177944816，共4798秒
- 数据文件总数：115152个

shard分组方法
- 按channel分组
- 每组40个，40秒一个shard，末尾可能不完全对齐
- 分组后shard文件名：1177938016/1177940019_1177940058_ch148.tar.zst

如何设计arcset的元数据定义及arcset gen-def命令参数，使得能支持以上的功能？


### 性能优化

- 打包性能优化
  - 利用本地存储
  -  
- 解包性能优化
  - ？


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

- iso9660 库（`github.com/kdomanski/iso9660`）`WriteTo()` 内部硬编码 `time.Now()`，写入 PVD 的 `VolumeCreationDateAndTime` / `VolumeModificationDateAndTime` / `VolumeEffectiveDateAndTime` 字段，导致每次生成的 ISO 镜像字节不同，SHA-256 checksum 不匹配。
  - 已 fork 到 `internal/iso9660/`，`ImageWriter` 增加 `Timestamp` 字段，`doMakeIsoShard` 传入 `time.Unix(0, 0)` 固定时间戳。
  - tar 无此问题，因 `archive/tar` 输出确定性。


## postgresql支持

- 已有代码的所有数据库操作，支持sqlite/postgresql
- 数据库导入/导出支持
  - sqlite -> postgres
  - postgres -> sqlite
  - 考虑到arcset-id可能存在冲突，需支持arcset-id的整体调整


## 功能特性

- 子命令的auto completion