# SKA 天文数据打包示例

## 数据特征

- dataset: 1177938016
- 文件名格式: `1177938016/<start_ts>_<end_ts>_ch<channel>.dat`
- channel: 133 ~ 156（24 个）
- 时间跨度: 1177940019 ~ 1177944816（4798 秒）
- 文件总数: 115152 个
- 单文件大小: 10 KB（模拟）
- 总容量: ~1.15 GB（模拟）

## 运行

```sh
# 1) 模拟数据
bash simulate.sh

# 2) 全流程打包（dataset create → gen-def → shard make → dataset validate → dataset finalize → unpack）
bash pack-serial.sh
```

环境变量：

| 变量 | 默认 | 说明 |
|------|------|------|
| `DATA_ROOT` | `./data` | 数据根目录 |
| `PACKFS_BIN` | `packfs` | packfs 二进制路径 |
| `SQLITE_DB` | `./packfs.db` | 数据库路径 |

## 测试结果（MacBook Pro M1，1.15 GB × 115152 文件）

| 步骤 | 耗时 | 说明 |
|------|------|------|
| dataset create | **11s** | 批量 INSERT（`PACKFS_BATCH_INSERT=199`），优化前 108s |
| gen-def | 0s | Python 脚本，直接生成 2880 个 .def |
| shard make | 1322s | 2880 个 shard 串行打包（瓶颈） |
| validate | 0s | SHA-256 校验 |
| finalize | 0s | 复制 DB + 归一 dataset_id |
| unpack | 109s | 解包验证 |
| **总计** | **1540s** | 115152 个文件全部校验通过 |

## 后续优化

- `shard make`（1322s，占 86%）— scalebox 分布式并行打包
- `unpack`（109s，7%）— 并行解包

## 文件说明

| 文件 | 用途 |
|------|------|
| `dataset.def` | 数据集配置（时间、channel、分组参数） |
| `simulate.sh` | 生成固定大小随机数据文件 |
| `gen-def.sh` | gen-def 脚本：按 channel 分组，40 文件/shard |
| `pack-serial.sh` | 完整串行流程 + 耗时统计 |
| `pack-scalebox.sh` | 分布式打包（TBD） |
| `unpack.sh` | 解包辅助 |
