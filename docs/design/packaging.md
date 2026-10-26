# 打包与格式

## 1. 压缩取值校验

现状：`parseCompressMode` 只识别 `zstd`/`xz`/`segment:zstd`/`segment:xz`，其他字符串静默按不压缩处理（`internal/shard/make.go:57`）；`dataset create --compress` 接受任意值，README/design 还宣传 `zstd_seekable`。

设计：

- 定义唯一合法集合；`dataset create`、`arcset append`、`shard make` 入口统一校验，非法值直接报错。
- `zstd_seekable`：要么实现（writer + FUSE/解包读取），要么从文档和 CLI 帮助中删除；不支持前必须拒绝。
- 建议短期：拒绝 `zstd_seekable`，保留 TODO；FUSE 随机读优先 `segment:*`。

## 2. dataset create 输出目录

现状：非 gen-only 时 shard 写回 `ds.current_path`（`cmd/cli/dataset/create.go:81`、`internal/api/handler.go:186`），源数据与 shard 混放；重新扫描会把 shard 当源文件。

设计：

- `dataset create` 增加 `--target-root`（API 增加 `target_root`）作为 shard 输出目录；dataset SQLite 由 `--db-dir` 单独指定，两者分开。缺省行为待定：
  - A：保持现状（兼容），但输出警告；
  - B：缺省写 `current_path/shards/`（更干净，改变现有路径）；
  - C：缺省必须显式指定，未指定则只扫描（等价 gen-only）。
- 推荐 B，并在 metadata 记录 `shard_root`，校验/unpack/FUSE 统一读取。
- WebUI 创建表单同步增加目标目录。

## 3. gen-def 灵活性（todo.md 待实现）

设计 CLI 参数（仅内置模式）：

- `--group-by=size|count`：按字节或文件数分组；
- `--files-per-shard=N`：count 模式的每组文件数；
- `--sort=path|size|mtime`：组内/组间排序；
- `--shard-name-template`：如 `{seq:04d}.{format}`，限制白名单变量。

外部脚本模式已支持任意分组，保持现状。

dataset 增加两个排序属性，决定 shard 进入 arcset 时的顺序：

- `shard_sort_regex`：从 shard 文件名提取排序字段的正则；
- `shard_sort_index`：逗号分隔的捕获组编号，按顺序比较。

未配置时按 `t_shard.seq` / .def 顺序。属性在 dataset 创建/定义时写入 metadata，CLI 可提供 `--shard-sort-regex`、`--shard-sort-index`。详见 [arcset-lifecycle.md](arcset-lifecycle.md) §4。

## 4. 并行打包

- 先生成全部 .def/分组，再用 worker pool 并行 `MakeShard`（`--parallel=N`）。
- 每个 dataset SQLite 单写者：DB 写入串行化（单 goroutine 或 mutex），文件写入并行。
- 保证输出顺序与串行一致（按 seq 命名，不依赖完成顺序）。
- 先做 CLI，再复用到 API；补 benchmark（10 万文件场景）。

## 5. ISO

- ISO 格式保留在 packfs 仓库中，后续可能不常用但不删除。
- 不做特殊处理（包括固定时间戳 / 可复现构建）；有实际需求再议。

## 6. 分布式打包（scalebox）

- `examples/astro/pack-scalebox.sh` 是占位脚本；`router/shard_make.go` 的 `fromShardMake` 是空实现。
- 设计：packfs CLI 保持单机执行；分布式由 scalebox 调用 `shard make --def-file`，主路由负责分片、等待、汇总。
- **控制面串行**：同一 arcset 的 `append` / `build-data` / `make-ec` / `expand` / `recover` / `export` 由 scalebox router 串行调用；packfs 不内置 arcset 锁，router 实现由 scalebox 项目负责。数据面 stripe 可并行。
- **路径可见性**：router/执行节点必须能访问 `t_dataset_ref.sqlite_path`；共享存储或本地副本由 scalebox 负责。
- **EC 流式**：大 shard 的编码/恢复/校验使用 [ec-streaming.md](ec-streaming.md) 的分块 API，`--ec-chunk-size` / `--ec-parallel` 由流水线传入。
- 不把 scalebox 依赖引入 packfs 核心包。
- 待决：是否在 packfs 内提供 `shard make-batch` 批量入口？

## 7. PAD shard 与命名（仅 m>0）

`m=0` 时不存在 EC/PAD，最后一条带可不足 k，不补 PAD。

- dataset 阶段：data shard 文件名不受限制（标准序号或用户脚本自定义），不带分区前缀。
- arcset 阶段：partition 目录（默认 2 位、最大 3 位）+ 文件名 seq 前缀：`<partition>/<seq>D-<dataset_key>-<原名>`、`<partition>/<seq>E.<ext>`、`<partition>/<seq>P.<ext>`。`seq` 固定宽度，默认 3，最大 5。
- PAD shard 要写入下游介质；packfs 侧生成 **0 字节空文件**，`padded_size` 记录在 metadata 与 `arcset.json`。
- PAD 只占条带位置，不消耗 `used_capacity`；新增 dataset 时优先复用 PAD 位置，并重算受影响 stripe 的 EC。
- recover 时 PAD 缺失按全零处理，可直接重新生成空文件。
- 下游存储系统必须接受 PAD 类型并把空文件写入对应分区（不能按“空文件无意义”跳过）。

## 待决问题

1. 压缩非法值：直接报错是否影响现有脚本？`zstd_seekable` 实现还是删除？
2. dataset create 缺省输出目录选 A/B/C？
3. 并行的默认线程数与 SQLite 写锁策略？
4. ISO 固定时间戳的时间基准（epoch / dataset create_time）？
