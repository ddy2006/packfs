#!/bin/bash
# SKA gen-def 脚本：按 channel 分组，40 文件/shard（不访问数据库）
# 调用: gen-def.sh [--id <arcset_id>] [--dataset-id <dataset_id>] [--target-root <output_dir>]

set -e

ARCSET_ID=1
DATASET_ID=1
TARGET_ROOT=
ARCSET_SET= DATASET_SET= TARGET_SET=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --id|--arcset-id) ARCSET_ID="$2"; ARCSET_SET=1; shift 2 ;;
    --dataset-id) DATASET_ID="$2"; DATASET_SET=1; shift 2 ;;
    --target-root) TARGET_ROOT="$2"; TARGET_SET=1; shift 2 ;;
    *) shift ;;
  esac
done

[ -z "$ARCSET_SET" ] && echo "WARN: --arcset-id not set, using default 1" >&2
[ -z "$DATASET_SET" ] && echo "WARN: --dataset-id not set, using default 1" >&2

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEF="$SCRIPT_DIR/dataset.def"
[ ! -f "$DEF" ] && { echo "dataset.def not found" >&2; exit 1; }

DS_NAME=$(awk '/^  name:/{print $2}' "$DEF")
FORMAT=$(awk '/^  format:/{print $2}' "$DEF")
COMPRESS=$(awk '/^  compress:/{print $2}' "$DEF")
[ -z "$FORMAT" ] && FORMAT=bin
[ -z "$COMPRESS" ] && COMPRESS=""
[ -z "$TARGET_ROOT" ] && TARGET_ROOT="./shard-def/$DS_NAME"
[ -z "$TARGET_SET" ] && echo "WARN: --target-root not set, using default $TARGET_ROOT" >&2

# 用 Python 生成 .def 文件
python3 - "$DEF" "$TARGET_ROOT" "$ARCSET_ID" "$DATASET_ID" "$FORMAT" "$COMPRESS" << 'PYEOF'
import sys, os, re

def_file, target_root, arcset_id, dataset_id = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
fmt = sys.argv[5] if len(sys.argv) > 5 else 'bin'
compress = sys.argv[6] if len(sys.argv) > 6 else ''

# 计算 .def 和 shard 文件扩展名，与 Go 端 compressExt/algoExt 保持一致
algo = ''
if compress in ('zstd', 'xz', 'zstd_seekable', 'segment:zstd', 'segment:xz'):
    algo = 'zst' if 'zstd' in compress else 'xz'

if compress in ('zstd', 'xz', 'zstd_seekable'):
    ext = f'{fmt}.{algo}'           # shard 级压缩: iso.zst
elif compress in ('segment:zstd', 'segment:xz'):
    ext = f'{algo}.{fmt}'           # segment 级压缩: zst.iso
else:
    ext = fmt                        # 无压缩: iso

config = {}
with open(def_file) as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        if ':' in line:
            k, v = line.split(':', 1)
            k, v = k.strip(), v.strip().strip('"')
            if v.isdigit():
                v = int(v)
            config[k] = v

name = str(config['name'])
start_ts = config['start_ts']
end_ts = config['end_ts']
ch_start = config['ch_start']
ch_end = config['ch_end']
group_size = config.get('group_size', 40)

os.makedirs(target_root, exist_ok=True)

count = 0
for ch in range(ch_start, ch_end + 1):
    ts = start_ts
    while ts <= end_ts:
        batch = []
        batch_start = ts
        for _ in range(group_size):
            if ts > end_ts:
                break
            next_ts = ts + 1
            path = f"{name}/{ts}_{next_ts}_ch{ch}.dat"
            batch.append(path)
            batch_end = next_ts
            ts = next_ts

        if batch:
            count += 1
            fname = f"{target_root}/{batch_start}_{batch_end}_ch{ch}.{ext}.def"
            with open(fname, 'w') as out:
                out.write(f"# arcset_id: {arcset_id}\n")
                out.write(f"# dataset_id: {dataset_id}\n")
                for p in batch:
                    out.write(p + '\n')

    sys.stderr.write(f"  ch{ch}: {count} shards\n")

print(count)

PYEOF
