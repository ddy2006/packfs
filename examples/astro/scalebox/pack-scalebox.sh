#!/bin/bash
# 分布式打包（scalebox）
# 后续实现：将 shard make 任务分发到 scalebox 集群并行执行
#
# 与 pack-serial.sh 的区别：
#   - arcset unpack 需要等待所有 shard 完成（依赖 scalebox sink 机制）
#   - 每个节点需挂载共享存储（NFS/CEPH）以访问数据源和输出目录

echo "pack-scalebox: not yet implemented" >&2
exit 1
