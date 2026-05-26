# internal

## dataset

- 数据集是inmmutable，生成后数据集内容不再修改。
- 增加数据集：从一个目录创建一个数据集，写入到t_dataset表中；将该目录中的所有文件也写入到t_file表中。
- 可以选择存放在postgresql或sqlite中。sqlite的文件通过环境变量SQLITE_DB指定，如果数据库文件不存在，则用build/sqlite/erd.sql创建sqlite数据库文件；
- dataset相关代码放在dataset包中。
