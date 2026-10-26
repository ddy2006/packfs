# EC 流式编解码

> 解决 100GB 级 shard 的 EC 编码/恢复/校验内存问题。当前 `internal/ec` 的 `EncodeStripe` / `ReconstructStripe` / `VerifyStripe` 使用 `os.ReadFile` 全量读入，k=8、单 shard 100GB 时不可用。

## 1. 目标与约束

- 单 shard 目标 100GB；k ≤ 24，m ∈ {0,2,4,6}，`k+m ≤ 24`。
- 编码/恢复/校验不得把整条带读入内存。
- 内存上限与 `(k+m) × chunk_size` 成正比，默认 chunk 8–16MiB。
- 保持 RS 数学不变：对 shard 的同一字节位置做线性组合，因此可以按 chunk 独立编码。
- `m=0` 时不做 EC，直接完成数据校验。

## 2. 参数

| 参数 | 缺省 | 说明 |
|------|------|------|
| `--ec-chunk-size` | 8MiB | 单次 RS 编码块大小；允许 1–64MiB |
| `--ec-parallel` | 1 | 并行处理的 stripe 数；受磁盘带宽限制 |

内存估算（k=8, m=4, chunk=8MiB）：约 `12 × 8MiB = 96MiB`，加文件缓冲；chunk=16MiB 约 192MiB。

## 3. 编码流程

输入：同一条带的 k 个 data shard（已移动/重命名到 arcset 的 partition 目录）和 m 个 EC 目标路径。

1. 打开 k 个 data shard，取得各自 `file_size`；`padded_size = max(file_size)`。
2. 创建 m 个 EC 文件（先写 `<path>.tmp`），截断到 0。
3. 循环 `offset = 0, chunk, 2*chunk, ... < padded_size`：
   - `n = min(chunk, padded_size - offset)`。
   - 从每个 data shard 读取最多 `n` 字节；不足部分补零，得到 k 个长度为 `n` 的 buffer。
   - 调用 `reedsolomon.Encode`，得到 m 个长度为 `n` 的 parity buffer。
   - 依次追加写入 m 个 EC 临时文件。
4. 最后一块按 `n = padded_size - offset` 编码并写入，EC 文件大小精确等于 `padded_size`，不按 chunk 向上取整。
5. 对每个 EC 临时文件 `fsync` 后 `rename` 到最终路径；失败保留临时文件供重试。
6. DATA shard 不重写；PAD 只在缺失数据位置生成 0 字节文件。

- EC 文件大小 = `padded_size`；DATA shard 的 `original_size` / `padded_size` 写入 PG 的 `metadata`。
- 同一 stripe 重跑覆盖同名临时/最终文件，幂等。

## 4. 恢复流程

输入：同一条带 k+m 个位置，缺失位置为空；至少 k 个可用。

1. 打开所有可用 shard；对缺失位置创建临时文件。
2. 循环 chunk：
   - `n = min(chunk, padded_size - offset)`。
   - 从可用 shard 读取 `n` 字节；缺失位置 buffer 填零；DATA 末尾不足补零。
   - 调用 `reedsolomon.Reconstruct` 重建缺失的 `n` 字节块。
   - 将重建块写入对应临时文件。
3. `--target=data`：只重建 DATA 位置；最后一个 chunk 按 `original_size` 截断。
4. `--target=all`：重建 DATA + EC；EC 文件保持 `padded_size`。
5. 每个重建文件完成后 `fsync` + `rename`；对 DATA 计算 SHA-256，必须与 PG 记录的 `sha256` 一致，否则失败。
6. checksum 不更新；只更新 PG `last_check`、`state` 和 arcset `health`。

## 5. 校验流程

1. 打开 k+m 个 shard；确定 `padded_size`。
2. 循环 chunk：
   - `n = min(chunk, padded_size - offset)`。
   - 读取每个 shard 的 `n` 字节；DATA 末尾补零。
   - 调用 `reedsolomon.Verify`；任一块不一致即失败。
3. 校验是只读的；可按 `--sample` 抽样（后续），首版全量按 chunk 校验。
4. 单 shard checksum 用流式 SHA-256，不加载整文件。

## 6. 与 placement/命名的关系

- `PlanStripes` 需要重写：
  - 0-based `stripe_seq` / `pos`；
  - 使用 partition 目录与 `<seq>D-...` / `<seq>E.<ext>` / `<seq>P.<ext>` 命名；
  - 不再使用旧的 `<stripe>D<position>_` 1-based 命名。
- 编码器只接收最终文件路径；placement 由 `arcset build-data` / `make-ec` 在 PG 中记录。
- EC/PAD 文件写入对应 partition 目录；PAD 0 字节。

## 7. 失败与恢复

- EC 文件先写 `.tmp`，成功后 rename；失败可重试整条 stripe。
- 不引入 chunk 级断点续传；重试成本由外部调度器控制。
- 若 `padded_size` 或 `original_size` 与 PG 不一致，编码/恢复直接失败，不写最终文件。
- EC 临时文件在 stripe 成功后清理；启动/重试时可清理同名 `.tmp`。

## 8. 代码改造范围

- `internal/ec/encode.go`：新增流式 `EncodeStripeStreaming`；旧 `EncodeStripe` 仅保留给小文件/测试或删除；重写同文件的 `PlanStripes` 为 0-based/partition 命名。
- `internal/ec/decode.go`：新增流式 `ReconstructStripeStreaming` / `VerifyStripeStreaming`。
- 调用方：`internal/shard/make_ec.go`、`arcset recover`、`arcset validate` 改为流式 API。
- 测试：用 2–4GiB 稀疏/生成文件做内存上限测试；用小文件做正确性测试；100GB 场景用 mock/chunk 计数验证不整文件读取。

## 9. 待确认

1. 默认 chunk 8MiB 还是 16MiB；是否按 `k+m` 自动调整。
2. 是否提供 `--ec-parallel`；并行 stripe 的磁盘带宽上限。
3. 是否保留旧的非流式 API 作为测试辅助。
