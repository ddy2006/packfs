# scalebox 分布式打包应用

## 数据介绍

- 原始数据：单数据集 36TB，4800 秒，24 通道，313MB/s
- 数据按秒分文件存储，支持两种形态：
  - **预打包**（`ORIGIN_PACKED=yes`）：每秒一个压缩包（如 `t<ts>.tar.zst`），内含 24 通道的 .dat 文件
  - **散文件**：每秒 24 个独立 .dat 文件，命名 `<ts_start>_<ts_end>_ch<channel_id>.dat`
- 打包装箱粒度由 `dataset.def` 中的 `shard.group_size` 控制（如 60 秒一个 shard）

## 全流程打包

```
dataset create → arcset create → arcset gen-def → scalebox app
  ├── pull-untar: 数据拷贝到处理节点，按需解包
  ├── shard-make:  shard 打包、校验、验证
  └── router:     路由模块，分发任务
→ arcset finalize
```

## dataset.def 配置

router 启动时加载的 YAML 配置文件（路径通过 `DEF_FILE` 环境变量指定，默认 `../../dataset.def`）：

```yaml
dataset:
  name: "1177938016"    # 数据集名称
  start_ts: 1177940019  # 起始时间戳
  end_ts: 1177944819    # 结束时间戳（不含）
  ch_start: 0           # 起始通道号
  ch_end: 23            # 结束通道号（含）

shard:
  group_by: "time"      # 分组维度
  group_size: 60        # 每组秒数（决定 shard 覆盖的时间窗口）
  compress: "zstd"      # 压缩算法
  format: "tar"         # shard 格式（bin / tar）

simulation:
  file_bytes: 131072    # 模拟数据时单文件大小（字节）
```

## 模块设计

### pull-untar

- 从 `source_root` 拉取数据文件到 `target_root`
- 支持三种文件类型：
  - `*.tar.zst`：zstd 解压 → tar 解包
  - `*.tar`：tar 解包
  - 其他：直接拷贝
- 可选带宽限制（`bw_limit`），通过 `pv -L` 实现
- 成功后写 `${WORK_DIR}/sink-tasks.txt`，触发 router 生成 shard-make 任务

**任务参数：**

| 参数 | 说明 |
|------|------|
| `source_root` | 源路径根目录 |
| `target_root` | 目标路径根目录 |
| `bw_limit` | 读取带宽上限（如 `500k`、`10m`） |

### shard-make

- 读取 .def 定义文件，执行三步操作：
  1. `packfs shard make --def-file=...`：打包生成 shard
  2. `packfs shard validate --shard-file=...`：校验 SHA-256
  3. `packfs shard unpack --shard-file=... --target-root=...`：解包验证
- shard 文件名由 def 文件名去掉 `.def` 后缀推导
- 任一步失败即退出

**任务参数：**

| 参数 | 说明 |
|------|------|
| `def_root` | shard 定义文件根目录 |
| `source_root` | 源路径根目录 |
| `target_root` | 目标路径根目录 |
| `bw_limit` | 读取带宽上限 |

### router

Go 程序，根据任务头 `from_module` 字段路由到对应处理函数：

| from_module | 处理函数 | 行为 |
|-------------|----------|------|
| `""`（null） | `fromNull` | 加载 dataset.def → 创建信号量 → 生成 pull-untar 任务 |
| `pull-untar` | `fromPullUntar` | 处理 pull-untar 返回，生成 shard-make 任务（桩） |
| `shard-make` | `fromShardMake` | 记录结果，更新信号量（桩） |

**关键环境变量：**

| 变量 | 说明 |
|------|------|
| `DEF_FILE` | dataset.def 文件路径 |
| `ORIGIN_PACKED` | `yes` = 原始数据为按秒打包的 tar.zst，否则为单通道散文件 |
| `ZSTD_COMPRESSED` | 数据是否 zstd 压缩 |
| `LOG_LEVEL` | 日志级别，默认 info |
| `APP_ID` | scalebox 应用 ID |

## 信号量设计

| 信号量 | 格式 | 初值 | 含义 |
|--------|------|------|------|
| `shard-ok` | `shard-ok:<dataset>/t<start>_<end>/ch<N>` | 时间段秒数 | 每个 shard 内的 segment 计数 |
| `arcset-ok` | `arcset-ok:<dataset>` | shard 总数 | arcset 级别的完成计数 |

## 任务体格式

- **pull-untar**（预打包）：`<dataset>/t<ts>`，如 `1177938016/t1177940019`
- **pull-untar**（散文件）：`<dataset>/t<ts>/ch<N>.dat`，如 `1177938016/t1177940019/ch0.dat`
- **shard-make**：`<dataset>/<shard_def_file>`，如 `1177938016/t1177940019_ch0.tar.zst.def`

## 启动运行

```sh
cd packfs/examples/astro/scalebox
./pack-scalebox.sh
```

> `pack-scalebox.sh` 尚未实现，当前为占位脚本。

## 前置依赖

- `packfs` 二进制在 PATH 中
- `SQLITE_DB` 环境变量指向数据库文件
- `dataset.def` YAML 配置文件（位于 scalebox 目录或通过 `DEF_FILE` 指定）
- scalebox 集群已配置（`scalebox.env` 中 `CLUSTER` 指向目标集群）
- router Docker 镜像已构建（`cd router && make`）
- 节点共享存储（NFS/CEPH）挂载 `source_root` 和 `target_root`
