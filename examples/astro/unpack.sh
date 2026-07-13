#!/bin/bash
# 解包 SKA 数据：从 shard 或 dataset 恢复原始文件
#
# Usage:
#   解包单个 shard:    bash unpack.sh shard --shard-file=/path/to/0000.tar.zst --target-root=/extract
#   解包整个 dataset:  bash unpack.sh dataset --id=<dataset_id> --target-root=/extract

set -e

PACKFS="${PACKFS_BIN:-packfs}"
MODE="$1"; shift

case "$MODE" in
  shard)
    "$PACKFS" shard unpack "$@"
    ;;
  dataset)
    "$PACKFS" dataset unpack "$@"
    ;;
  *)
    echo "Usage: $0 <shard|dataset> <args...>" >&2
    exit 1
    ;;
esac
