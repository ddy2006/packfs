#!/bin/bash
# 解包 SKA 数据：从 arcset 恢复原始文件
#
# Usage:
#   解包单个 shard:  bash unpack.sh shard /path/to/0000.tar.zst --target-root=/extract
#   解包整个 arcset: bash unpack.sh arcset --name=ska-arcset --source-root=/data/output --target-root=/extract

set -e

PACKFS="${PACKFS_BIN:-packfs}"
MODE="$1"; shift

case "$MODE" in
  shard)
    "$PACKFS" shard unpack "$@"
    ;;
  arcset)
    "$PACKFS" arcset unpack "$@"
    ;;
  *)
    echo "Usage: $0 <shard|arcset> <args...>" >&2
    exit 1
    ;;
esac
