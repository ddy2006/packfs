# shard-make

## 模块功能

- dat文件打包为shard文件
- 验证打包结构的校验码
- unpack验证
- 删除中间文件


## 任务体body

body是shard定义文件名的文件相对路径
`<dataset-id>/<shard-def-file>`

## 主要参数表

参数名       | 参数说明
----------  | --------------------
def_root    | shard定义文件根目录
source_root | 源路径根目录
target_root | 目标路径根目录
bw_limit    | 读取带宽的最大值(MB/s，kB/s)，比如500k,10m

- 参数值可以从任务头、环境变量中读取
