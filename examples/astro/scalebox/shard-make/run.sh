#!/usr/bin/env bash

set -e
source /usr/local/lib/scalebox/functions.sh

body=$1
def_root=$(scalebox::task_header "$2" "def_root")
source_root=$(scalebox::task_header "$2" "source_root")
target_root=$(scalebox::task_header "$2" "target_root")
bw_limit=$(scalebox::task_header "$2" "bw_limit")

def_file="${def_root}/${body}"

# shard make: 从def文件打包生成shard
packfs shard make --def-file="$def_file"

# 从def文件名推导shard文件名（去掉 .def 后缀）
shard_name=$(basename "$body" .def)
shard_file="${target_root}/${shard_name}"

# 验证shard校验码
packfs shard validate --shard-file="$shard_file"

# unpack验证文件完整性
packfs shard unpack --shard-file="$shard_file" --target-root="${target_root}"

echo "$body" > ${WORK_DIR}/sink-tasks.txt
exit 0
