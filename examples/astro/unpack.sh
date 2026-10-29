#!/bin/bash
# 解包 SKA 数据（新 CLI：per-dataset SQLite）
#
# Usage:
#   bash unpack.sh --dataset-db /data/datasets/<uid>.db --target-root /extract

set -e

PACKFS="${PACKFS_BIN:-packfs}"
"$PACKFS" dataset unpack "$@"
