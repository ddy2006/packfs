# SKA 天文数据打包示例

## 数据特征

- dataset: 1177938016
- 文件名格式: `1177938016/<start_ts>_<end_ts>_ch<channel>.dat`
- channel: 133 ~ 156（24 个）
- 时间跨度可配置，当前 `dataset.def` 为 80 秒
- 文件总数与总容量由 `dataset.def` 决定

## 运行

```sh
# 1) 模拟数据
bash simulate.sh

# 2) dataset 串行打包 + 解包校验
bash pack-serial.sh

# 3) 完整流程（含 arcset / EC / recover）
export PACKFS_PG_URL='postgres://packfs:changeme@127.0.0.1:5432/packfs?sslmode=disable'
bash full-pipeline.sh --with-ec

# 清理
bash full-pipeline.sh --clean
```

环境变量：

| 变量 | 默认 | 说明 |
|------|------|------|
| `PACKFS_BIN` | `packfs` | packfs 二进制路径 |
| `DATA_ROOT` | `./data` | 数据根目录 |
| `PACKFS_PG_URL` | — | PostgreSQL DSN；arcset/EC 流程必需 |
| `PACKFS_DATASET_DB_DIR` | — | `dataset create` 的默认 SQLite 目录 |
| `SHARD_MAX_BYTES` | `10MiB` | dataset shard 上限 |
| `PARTITION_CAPACITY` | `10GB` | arcset 单分区容量 |

`full-pipeline.sh` 会实际执行：

```text
simulate
→ dataset create
→ gen-def
→ shard make
→ dataset mark-built
→ dataset unpack（移动前校验）
→ arcset create / append / build-data
→ arcset make-ec
→ arcset validate
→ arcset export
→ 删除一个 DATA shard → arcset recover → validate
```

## 文件说明

| 文件 | 用途 |
|------|------|
| `dataset.def` | 数据集配置（时间、channel、分组参数） |
| `simulate.sh` | 生成固定大小随机数据文件 |
| `gen-def.sh` | 按 channel 分组，40 文件/shard，写 `# dataset_db` |
| `pack-serial.sh` | dataset 串行打包 + 解包校验 |
| `full-pipeline.sh` | dataset + arcset + EC + recover 全流程 |
| `unpack.sh` | 解包辅助 |
| `scalebox/` | 分布式打包示例（命令需同步新 CLI） |

## 验收标准

- `dataset mark-built` 校验全部 DATA shard SHA-256 通过
- 解包文件数与原始文件数一致
- `arcset validate --stage=all` 通过
- 删除一个 DATA shard 后 `arcset recover` 恢复；SHA-256 与 `arcset.json` 一致
- `arcset.json` 可重复导出
