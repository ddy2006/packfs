# internal

## dataset

- 数据集是inmmutable，生成后数据集内容不再修改。
- 增加数据集：从一个目录创建一个数据集，写入到t_dataset表中；将该目录中的所有文件也写入到t_file表中。
- 可以选择存放在postgresql或sqlite中。sqlite的文件通过环境变量SQLITE_DB指定，如果数据库文件不存在，则用build/sqlite/erd.sql创建sqlite数据库文件；生产代码通过 `internal/db/schema.sql` 嵌入式 schema 自动建表。
- dataset相关代码放在dataset包中。


## arcset（归档集）

- arcset基于多个dataset创建，也是不变的
- arcset是多个dataset的容器
- arcset由多个shard组成

- arcset操作包括
  - 创建arcset（插入t_arcset纪录）
    - 生成所有shard创建所需的列表（segment描述），以便于分布式生成shard
  - 列出所有arcset
  - 列出某个arcset中所有dataset

## shard

- shard，具体来说就是打包文件，dataset中的多个文件组成
- 一个shard包含多个segment

- shard操作：
  - 基于segment描述，创建shard(t_shard、t_segment插入纪录，生成shard文件)

## segment
- segment对应dataSet中的单个文件
- 也可以考虑支持文件片段（不分文件），以支持超大文件做成shard

