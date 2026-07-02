# CLAUDE.md

This file provides guidance to Claude Code when working in `examples/astro/scalebox/`.

## 架构

scalebox 分布式打包应用，将 packfs 的 shard 打包流程分发到集群并行执行。

```
pack-scalebox.sh
  └── scalebox app (app.yaml)
        ├── router (null → pull-untar → shard-make)
        ├── pull-untar: 数据拷贝 + 解包
        └── shard-make: shard 打包 + 校验 + 验证
```

## 模块

| 模块 | 入口 | 职责 |
|------|------|------|
| `pull-untar` | `run.sh` | 从 source_root 拉取数据到 target_root，按需解压/解包 |
| `shard-make` | `run.sh` | 读取 .def 文件，执行 shard make/validate/unpack |
| `router` | `main.go` | 路由模块，根据 from_module 头分发任务 |

## 路由流程

### fromNull（`from_module=""`）

1. 从 `DEF_FILE` 环境变量指定的 YAML 文件（默认 `../../dataset.def`）加载配置
2. 按 `shard.group_size` 和通道范围遍历，创建信号量：
   - `shard-ok:<dataset>/t<start>_<end>/ch<N>`，初值为时间段内的秒数
   - `arcset-ok:<dataset>`，初值为 shard 总数
3. 根据 `ORIGIN_PACKED` 环境变量生成 pull-untar 任务：
   - `yes`：数据已预打包（如 tar.zst），任务体为 `dataset/t<ts>`（按秒）
   - 其他/未设置：数据为单通道文件，任务体为 `dataset/t<ts>/ch<N>.dat`

### fromPullUntar（`from_module="pull-untar"`）

接收 pull-untar 完成通知，按需生成 shard-make 任务。（当前为桩实现）

### fromShardMake（`from_module="shard-make"`）

记录 shard-make 结果，更新信号量。（当前为桩实现）

## dataset.def（YAML 配置）

```yaml
dataset:
  name: "1177938016"    # 数据集名称
  start_ts: 1177940019  # 起始时间戳
  end_ts: 1177944819    # 结束时间戳（不含）
  ch_start: 0           # 起始通道号
  ch_end: 23            # 结束通道号（含）

shard:
  group_by: "time"      # 分组维度（time / channel）
  group_size: 60        # 每组秒数
  compress: "zstd"      # 压缩算法
  format: "tar"         # shard 格式（bin / tar）

simulation:
  file_bytes: 131072    # 模拟数据文件大小（字节）
```

router 启动时通过 `init()` 加载该配置到 `defConfig *DatasetDef` 全局变量。

## 环境变量

| 变量 | 模块 | 说明 |
|------|------|------|
| `DEF_FILE` | router | dataset.def 文件路径，默认 `../../dataset.def` |
| `ORIGIN_PACKED` | router, pull-untar | 原始数据是否已打包（`yes` = 按秒打包的 tar.zst） |
| `ZSTD_COMPRESSED` | router | 数据是否 zstd 压缩 |
| `LOG_LEVEL` | router | 日志级别（debug/info/warn/error），默认 info |
| `APP_ID` | router | scalebox 应用 ID |
| `CLUSTER` | 全局 | scalebox 集群名，`scalebox.env` 中设置为 `local` |
| `WORK_DIR` | pull-untar, shard-make | 工作目录，sink-tasks.txt 输出路径 |

## 任务体格式

- **pull-untar**：`source_root` 下文件相对路径
  - 预打包数据（ORIGIN_PACKED=yes）：`<dataset>/t<ts>`（如 `1177938016/t1177940019`）
  - 单通道文件：`<dataset>/t<ts>/ch<N>.dat`（如 `1177938016/t1177940019/ch0.dat`）
- **shard-make**：`def_root` 下 .def 文件相对路径（如 `1177938016/t1177940019_ch0.tar.zst.def`）

## 关键文件

| 文件 | 说明 |
|------|------|
| `app.yaml` | scalebox 应用定义，声明模块、镜像、槽位；router 模块含 `ORIGIN_PACKED`、`ZSTD_COMPRESSED` 等环境变量 |
| `scalebox.env` | 环境变量，当前设置 `CLUSTER=local` |
| `pack-scalebox.sh` | 启动脚本（尚未实现） |
| `dataset.def` | YAML 格式的数据集定义文件，描述时间范围、通道、shard 分组策略 |

## 构建 Router

```sh
cd router
make          # 构建 Docker 镜像 packfs/router-packfs:latest
```

Makefile 在仓库根目录构建（`-f Dockerfile ../../../..`），Dockerfile 使用多阶段构建：
- 阶段一：`golang:1.25.2`，复制 `go.*` + router 源码，编译二进制
- 阶段二：`debian:13-slim`，复制 router 二进制 + scalebox agent，ENTRYPOINT 为 `goagent`

## Router 依赖

| 依赖 | 用途 |
|------|------|
| `github.com/kaichao/scalebox` | scalebox 公共库（信号量创建 `semaphore.CreateSemaphores`） |
| `github.com/kaichao/gopkg` | 错误包装（`errors.WrapE`）+ 日志 |
| `github.com/sirupsen/logrus` | 结构化日志 |
| `gopkg.in/yaml.v3` | YAML 解析（dataset.def） |

## 模块间通信

每个模块完成任务后，将下一阶段的任务体写入 `${WORK_DIR}/sink-tasks.txt`，router 读取后生成对应模块的新任务。

## 信号量

| 信号量 | 格式 | 初值 |
|--------|------|------|
| `shard-ok` | `shard-ok:<dataset>/t<start>_<end>/ch<N>` | shard 中 segment 数量（时间段内的秒数） |
| `arcset-ok` | `arcset-ok:<dataset>` | shard 总数 |

信号量通过 `semaphore.CreateSemaphores()` 创建，传入 appID 和超时时间（100s）。
