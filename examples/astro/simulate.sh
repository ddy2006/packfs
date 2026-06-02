#!/bin/bash
# 根据 dataset.def 生成仿真数据文件
# Usage: bash simulate.sh <def-file> <data-root>

set -e

DEF="${1:-dataset.def}"
DATA_ROOT="${2:-./shard-data}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

python3 - "$DEF" "$DATA_ROOT" << 'PYEOF'
import sys, os

def_file, data_root = sys.argv[1], sys.argv[2]

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
file_bytes = config.get('file_bytes', 1024)

data = os.urandom(file_bytes)
base = os.path.join(data_root, name)
os.makedirs(base, exist_ok=True)

total = 0
print(f"Dataset : {name}")
print(f"Time    : {start_ts} - {end_ts}")
print(f"Channels: {ch_start} - {ch_end}")
print(f"File size: {file_bytes} bytes")

for ch in range(ch_start, ch_end + 1):
    count = 0
    for ts in range(start_ts, end_ts + 1):
        next_ts = ts + 1
        fname = f"{name}/{ts}_{next_ts}_ch{ch}.dat"
        with open(os.path.join(data_root, fname), 'wb') as f:
            f.write(data)
        total += 1
        count += 1
    print(f"  ch{ch}: {count} files")

print(f"Total: {total} files in {base}")
PYEOF
