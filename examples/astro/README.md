# SKA 天文数据打包示例

## 数据特征

- dataset: 1177938016
- 文件名格式: `1177938016/<start_ts>_<end_ts>_ch<channel>.dat`
- channel: 133 ~ 156（24 个）
- 时间跨度: 1177940019 ~ 1177944816（4798 秒）
- 文件总数: 115152 个
- 总容量: ~36 TB

## 打包策略

- 按 channel 分组
- 同 channel 内按时序排序
- 每 40 个文件一个 shard
- 文件名: `1177938016/<min_ts>_<max_ts>_ch<channel>.tar.zst`

## 快速开始

```sh
# 1) 模拟数据目录（生成空文件，仅创建目录结构和文件名）
bash simulate.sh /data/ska

# 2) 创建 dataset
packfs dataset create --root-dir=/data/ska --name=ska-dataset

# 3) 创建 arcset
packfs arcset create --name=ska-arcset --target-root=/data/ska-output \
  --dataset-ids=1 --format=tar --compress=zstd

# 4) 生成 shard 定义
packfs arcset gen-def --id=1 --target-root=/data/ska-output \
  --script=./gen-def.sh

# 5a) 串行打包
bash pack-serial.sh /data/ska-output

# 5b) 分布式打包（scalebox）
bash pack-scalebox.sh /data/ska-output
```

## 文件说明

| 文件 | 用途 |
|------|------|
| `simulate.sh` | 模拟生成文件目录结构 |
| `gen-def.sh` | gen-def 脚本：按 channel + 时序分组 |
| `pack-serial.sh` | 串行打包 |
| `pack-scalebox.sh` | 分布式打包（scalebox） |
| `unpack.sh` | 解包示例 |
