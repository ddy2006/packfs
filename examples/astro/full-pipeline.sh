#!/bin/bash
# =============================================================================
# packfs 全流程脚本：从零生成数据 → 打包 → EC → 解包验证
#
# 覆盖所有 packfs 核心命令：
#   simulate → dataset create → gen-def → shard make
#   → dataset validate → dataset finalize
#   → arcset create → arcset append → shard make-ec → shard recover
#   → shard unpack → 校验完整性
#
# Usage:
#   # 基础打包流程（不含 EC）
#   bash full-pipeline.sh
#
#   # 含 EC 纠删码全流程
#   bash full-pipeline.sh --with-ec
#
#   # 自定义 EC 参数
#   bash full-pipeline.sh --with-ec --ec=8+4
#
#   # 仅清理
#   bash full-pipeline.sh --clean
#
# 环境变量:
#   PACKFS_BIN    packfs 二进制路径（默认 packfs）
#   SQLITE_DB     数据库路径（默认 ./packfs.db）
#   DATA_ROOT     数据根目录（默认 ./data）
#   SKIP_SIMULATE 跳过 simulate（默认空=执行）
# =============================================================================

set -e

# ─── 参数解析 ────────────────────────────────────────────────────────────────
WITH_EC=""
EC_CONFIG="4+2"
CLEAN_ONLY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-ec)    WITH_EC=1; shift ;;
    --ec)         EC_CONFIG="$2"; shift 2 ;;
    --ec=*)       EC_CONFIG="${1#*=}"; shift ;;
    --clean)      CLEAN_ONLY=1; shift ;;
    *)            echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# ─── 环境变量 ────────────────────────────────────────────────────────────────
PACKFS="${PACKFS_BIN:-packfs}"
export SQLITE_DB="${SQLITE_DB:-$(pwd)/packfs.db}"
DATA_ROOT="${DATA_ROOT:-$(pwd)/data}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEF_FILE="${1:-$SCRIPT_DIR/dataset.def}"

DAT_DIR="$DATA_ROOT/dat"
DEF_DIR="$DATA_ROOT/def"
SHARD_DIR="$DAT_DIR"
UNPACK_DIR="$DATA_ROOT/unpack"

# ─── 读取 dataset.def 配置 ───────────────────────────────────────────────────
parse_def() {
  local key="$1"
  awk -F':' "/^[[:space:]]*${key}:/{gsub(/\"/,\"\",\$2); sub(/^[[:space:]]+/,\"\",\$2); print \$2}" "$DEF_FILE" | xargs
}

DS_NAME=$(parse_def name)
FORMAT=$(parse_def format)
COMPRESS=$(parse_def compress)
START_TS=$(parse_def start_ts)
END_TS=$(parse_def end_ts)
CH_START=$(parse_def ch_start)
CH_END=$(parse_def ch_end)
GROUP_SIZE=$(parse_def group_size)
FILE_BYTES=$(parse_def file_bytes)

[ -z "$FORMAT" ] && FORMAT=bin
[ -z "$COMPRESS" ] && COMPRESS=""

# ─── 计算压缩后缀 ────────────────────────────────────────────────────────────
calc_suffix() {
  case "$COMPRESS" in
    zstd|xz|zstd_seekable) echo "${FORMAT}.$(echo "$COMPRESS" | sed 's/zstd/zst/')" ;;
    segment:zstd|segment:xz) echo "$(echo "$COMPRESS" | sed 's/segment://; s/zstd/zst/').$FORMAT" ;;
    *) echo "$FORMAT" ;;
  esac
}
SHARD_SUFFIX=$(calc_suffix)

# ─── 清理 ────────────────────────────────────────────────────────────────────
do_clean() {
  echo "=== 清理 ==="
  rm -f "$SQLITE_DB"
  rm -rf "$DATA_ROOT"
  echo "已删除: $SQLITE_DB, $DATA_ROOT (含 dataset/shard/def/unpack/arcset)"
  echo "清理完成。"
}

if [ -n "$CLEAN_ONLY" ]; then
  do_clean
  exit 0
fi

# ╔══════════════════════════════════════════════════════════════════════════════╗
# ║                        全流程开始                                           ║
# ╚══════════════════════════════════════════════════════════════════════════════╝

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║          packfs Full Pipeline                            ║"
echo "╠══════════════════════════════════════════════════════════╣"
echo "║ Dataset   : $DS_NAME"
echo "║ Channels  : $CH_START–$CH_END ($((CH_END - CH_START + 1)) ch)"
echo "║ Time      : $START_TS–$END_TS ($((END_TS - START_TS + 1)) 秒/ch)"
echo "║ Format    : $FORMAT"
echo "║ Compress  : ${COMPRESS:-none}"
echo "║ Files/ch  : $GROUP_SIZE"
echo "║ Data dir  : $DAT_DIR"
echo "║ Shard dir : $SHARD_DIR"
echo "║ Unpack dir: $UNPACK_DIR"
[ -n "$WITH_EC" ] && echo "║ EC config : $EC_CONFIG"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

START_TOTAL=$(date +%s)

# 清理旧数据
do_clean
mkdir -p "$DAT_DIR" "$DEF_DIR" "$SHARD_DIR" "$UNPACK_DIR"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 1: 生成仿真数据
# ═══════════════════════════════════════════════════════════════════════════════
if [ -z "$SKIP_SIMULATE" ]; then
  echo ">>> [1/8] 生成仿真数据 (simulate)"
  echo "     $FILE_BYTES bytes/file, $((CH_END - CH_START + 1)) channels × $((END_TS - START_TS + 1)) timestamps"
  START=$SECONDS

  python3 - "$DEF_FILE" "$DAT_DIR" << 'PYEOF'
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
for ch in range(ch_start, ch_end + 1):
    for ts in range(start_ts, end_ts + 1):
        next_ts = ts + 1
        fname = f"{name}/{ts}_{next_ts}_ch{ch}.dat"
        with open(os.path.join(data_root, fname), 'wb') as f:
            f.write(data)
        total += 1

print(f"     ✓ 生成 {total} 个文件 ({file_bytes} bytes/ea)")
PYEOF

  ORIG_COUNT=$(find "$DAT_DIR/$DS_NAME" -type f | wc -l | tr -d ' ')
  echo "     耗时: ${SECONDS}s, 文件总数: $ORIG_COUNT"
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Step 2: dataset create（扫描目录 + 写入存储配置，不生成 shard）
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo ">>> [2/8] dataset create (--gen-only)"
START=$SECONDS

"$PACKFS" dataset create \
  --root-dir="$DAT_DIR" \
  --name="$DS_NAME" \
  --format="$FORMAT" \
  --compress="$COMPRESS" \
  --gen-only

DS_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_dataset WHERE name='$DS_NAME'")
DB_FILE_COUNT=$(sqlite3 "$SQLITE_DB" "SELECT COUNT(*) FROM t_file")
DB_TOTAL_BYTES=$(sqlite3 "$SQLITE_DB" "SELECT json_extract(metadata, '$.total_bytes') FROM t_dataset WHERE id=$DS_ID")
echo "     dataset_id=$DS_ID, DB文件数=$DB_FILE_COUNT, 总字节=$DB_TOTAL_BYTES"
echo "     耗时: ${SECONDS}s"

# ─── 校验：文件数量一致 ───
if [ "$ORIG_COUNT" != "$DB_FILE_COUNT" ]; then
  echo "     ✗ 错误: 磁盘文件数($ORIG_COUNT) != DB记录数($DB_FILE_COUNT)" >&2
  exit 1
fi
echo "     ✓ 磁盘文件数 = DB 记录数 = $ORIG_COUNT"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 3: gen-def（自定义脚本：按 channel 分组，$GROUP_SIZE 文件/shard）
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo ">>> [3/8] gen-def (按 channel 分组, $GROUP_SIZE 文件/shard)"
START=$SECONDS

SHARD_COUNT=$(python3 - "$DEF_FILE" "$DEF_DIR" "$DS_ID" "$FORMAT" "$COMPRESS" << 'PYEOF'
import sys, os, re

def_file, target_root, dataset_id = sys.argv[1], sys.argv[2], sys.argv[3]
fmt = sys.argv[4] if len(sys.argv) > 4 else 'bin'
compress = sys.argv[5] if len(sys.argv) > 5 else ''

# 计算扩展名
algo = ''
if compress in ('zstd', 'xz', 'zstd_seekable', 'segment:zstd', 'segment:xz'):
    algo = 'zst' if 'zstd' in compress else 'xz'
if compress in ('zstd', 'xz', 'zstd_seekable'):
    ext = f'{fmt}.{algo}'
elif compress in ('segment:zstd', 'segment:xz'):
    ext = f'{algo}.{fmt}'
else:
    ext = fmt

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
                out.write(f"# dataset_id: {dataset_id}\n")
                for p in batch:
                    out.write(p + '\n')

    sys.stderr.write(f"  ch{ch}: {count} shards\n")

print(count)
PYEOF
)

echo "     耗时: ${SECONDS}s, def 文件数: $(ls "$DEF_DIR"/*.def 2>/dev/null | wc -l)"

# ─── 校验：def 文件数 = Channel 数 ───
EXPECTED_DEF=$((CH_END - CH_START + 1))
ACTUAL_DEF=$(ls "$DEF_DIR"/*.def 2>/dev/null | wc -l)
if [ "$EXPECTED_DEF" != "$ACTUAL_DEF" ]; then
  echo "     ✗ 错误: 期望 $EXPECTED_DEF 个 def 文件, 实际 $ACTUAL_DEF" >&2
  exit 1
fi
echo "     ✓ def 文件数 = Channel 数 = $ACTUAL_DEF"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 4: shard make（串行打包所有 .def → shard）
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo ">>> [4/8] shard make ($ACTUAL_DEF shards)"
START=$SECONDS
i=0
for def in $(ls "$DEF_DIR"/*.def 2>/dev/null | sort); do
  i=$((i + 1))
  printf "     [%3d/%3d] %s ... " "$i" "$ACTUAL_DEF" "$(basename "$def")"
  if "$PACKFS" shard make --def-file="$def" --output-dir="$SHARD_DIR" 2>&1 | grep -q "created shard"; then
    echo "✓"
  else
    echo "✗"
    echo "     错误: shard make 失败" >&2
    exit 1
  fi
done

SHARD_FILES=$(ls "$SHARD_DIR"/*."$SHARD_SUFFIX" 2>/dev/null | wc -l)
echo "     耗时: ${SECONDS}s, shard 文件数: $SHARD_FILES"

# 列出 shard 清单
echo ""
echo "     Shard 清单:"
sqlite3 "$SQLITE_DB" \
  "SELECT printf('       %-55s %7d bytes  sha256=%.16s...',
          file_path, file_size, sha256)
   FROM t_shard WHERE dataset=$DS_ID ORDER BY seq, file_path"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 5: dataset validate（校验所有 shard SHA-256）
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo ">>> [5/8] dataset validate"
START=$SECONDS
"$PACKFS" dataset validate --id="$DS_ID" --source-root="$SHARD_DIR"
echo "     耗时: ${SECONDS}s"
echo "     ✓ 所有 shard SHA-256 校验通过"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 6: dataset finalize（封存：复制 DB → 标记 archived）
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo ">>> [6/8] dataset finalize"
START=$SECONDS
"$PACKFS" dataset finalize --id="$DS_ID" --source-root="$SHARD_DIR"
DS_STATUS=$(sqlite3 "$SQLITE_DB" "SELECT status FROM t_dataset WHERE id=$DS_ID")
echo "     耗时: ${SECONDS}s, dataset 状态: $DS_STATUS"
echo "     ✓ Dataset 已封存，目录自包含"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 7: 解包验证（EC 之前，直接用原始 shard）
# ═══════════════════════════════════════════════════════════════════════════════
STEP_NUM=7
TOTAL_STEPS=$([ -n "$WITH_EC" ] && echo "10" || echo "7")
echo ""
echo ">>> [${STEP_NUM}/${TOTAL_STEPS}] shard unpack + 完整性校验"
START=$SECONDS

# 解包所有 shard
rm -rf "$UNPACK_DIR"
mkdir -p "$UNPACK_DIR"

shopt -s nullglob
shards=( "$SHARD_DIR"/*."$SHARD_SUFFIX" )
shopt -u nullglob

for shard in "${shards[@]}"; do
  "$PACKFS" shard unpack --shard-file="$shard" --target-root="$UNPACK_DIR" 2>&1 | grep -v "level=" || true
done
echo "     已解包 ${#shards[@]} 个 shard → $UNPACK_DIR"

# ─── 校验 1：文件数量 ───
UNPACK_COUNT=$(find "$UNPACK_DIR" -type f | wc -l | tr -d ' ')
echo ""
echo "     ┌─────────────────────────────────────┐"
printf "     │ 原始文件数:  %6d                  │\n" "$ORIG_COUNT"
printf "     │ 解包文件数:  %6d                  │\n" "$UNPACK_COUNT"
echo "     └─────────────────────────────────────┘"

if [ "$ORIG_COUNT" -eq "$UNPACK_COUNT" ]; then
  echo "     ✓ 文件数量一致"
else
  echo "     ✗ 文件数量不一致!" >&2
  exit 1
fi

# ─── 校验 2：逐文件 MD5 对比 ───
echo ""
echo "     逐文件校验中..."
MISMATCH_COUNT=0

# 用临时文件缓存原始文件 MD5
ORIG_MD5=$(mktemp)
find "$DAT_DIR/$DS_NAME" -type f -exec md5sum {} \; | sort -k2 > "$ORIG_MD5"

UNPACK_MD5=$(mktemp)
find "$UNPACK_DIR" -type f -exec md5sum {} \; | sed "s|$UNPACK_DIR|$DAT_DIR|g" | sort -k2 > "$UNPACK_MD5"

if diff -q "$ORIG_MD5" "$UNPACK_MD5" > /dev/null 2>&1; then
  echo "     ✓ 所有文件 MD5 一致"
else
  MISMATCH_COUNT=$(diff "$ORIG_MD5" "$UNPACK_MD5" | grep "^[<>]" | wc -l | tr -d ' ')
  echo "     ✗ $MISMATCH_COUNT 个文件 MD5 不一致!" >&2
  diff "$ORIG_MD5" "$UNPACK_MD5" | head -20
  rm -f "$ORIG_MD5" "$UNPACK_MD5"
  exit 1
fi
rm -f "$ORIG_MD5" "$UNPACK_MD5"

	echo "     耗时: ${SECONDS}s"

# ═══════════════════════════════════════════════════════════════════════════════
# Step 8-10: EC 纠删码（可选，仅 --with-ec）
# ═══════════════════════════════════════════════════════════════════════════════
RECOVER_OK=""
if [ -n "$WITH_EC" ]; then
  STEP_NUM=8
  echo ""
  echo ">>> [${STEP_NUM}/${TOTAL_STEPS}] EC: arcset create + append"
  START=$SECONDS
  ARC_DIR="$DATA_ROOT/arcset"
  mkdir -p "$ARC_DIR"
  "$PACKFS" arcset create --name="${DS_NAME}-ec" --target-root="$ARC_DIR" --ec="$EC_CONFIG"
  AS_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_arcset WHERE name='${DS_NAME}-ec'")
  "$PACKFS" arcset append --id="$AS_ID" --dataset-id="$DS_ID"
  echo "     arcset_id=$AS_ID, dataset_id=$DS_ID, ec=$EC_CONFIG"
  echo "     耗时: ${SECONDS}s"

  STEP_NUM=9
  echo ""
  echo ">>> [${STEP_NUM}/${TOTAL_STEPS}] EC: shard make-ec"
  START=$SECONDS
  "$PACKFS" shard make-ec --arcset-id="$AS_ID"
  TOTAL_EC_SHARDS=$(sqlite3 "$SQLITE_DB" "SELECT COUNT(*) FROM t_shard WHERE arcset=$AS_ID")
  K=$(echo "$EC_CONFIG" | cut -d+ -f1)
  M=$(echo "$EC_CONFIG" | cut -d+ -f2)
  echo "     耗时: ${SECONDS}s, EC shard: $TOTAL_EC_SHARDS (${K}D+${M}E)"
  echo "     Arcset 目录:"
  ls -la "$ARC_DIR"/*.tar* 2>/dev/null | while read line; do
    echo "       $(echo "$line" | awk '{print $5, $NF}')"
  done

  STEP_NUM=10
  echo ""
  echo ">>> [${STEP_NUM}/${TOTAL_STEPS}] EC: 恢复测试"
  START=$SECONDS
  LOST_SHARD=$(sqlite3 "$SQLITE_DB"     "SELECT file_path FROM t_shard WHERE arcset=$AS_ID AND type='DATA' ORDER BY id LIMIT 1")
  LOST_PATH="$ARC_DIR/$LOST_SHARD"
  LOST_SIZE=$(stat -c%s "$LOST_PATH")
  echo "     目标: $LOST_SHARD ($LOST_SIZE bytes)"
  rm "$LOST_PATH"
  echo "     已删除, 开始恢复..."
  "$PACKFS" shard recover --arcset-id="$AS_ID" --shard-file="$LOST_SHARD"
  RECOVERED_SIZE=$(stat -c%s "$LOST_PATH")
  if [ "$LOST_SIZE" = "$RECOVERED_SIZE" ]; then
    echo "     ✓ 恢复成功 ($RECOVERED_SIZE bytes)"
    RECOVER_OK=1
  else
    echo "     ✗ 恢复失败: 期望 $LOST_SIZE, 实际 $RECOVERED_SIZE" >&2
    exit 1
  fi
  echo "     耗时: ${SECONDS}s"
fi

# ═══════════════════════════════════════════════════════════════════════════════
# 完成
# ═══════════════════════════════════════════════════════════════════════════════
TOTAL_TIME=$(($(date +%s) - START_TOTAL))
EC_STATUS="未启用"
[ -n "$RECOVER_OK" ] && EC_STATUS="$EC_CONFIG (✓ 恢复测试通过)"

# ─── 总结框（Python 渲染，CJK 字符对齐） ─────────────────────────────────────
python3 << PYEOF
import sys, os

total_time = $TOTAL_TIME
orig_count = $ORIG_COUNT
shard_files = $SHARD_FILES
ec_status = """$EC_STATUS"""
file_check = "全部通过 (数量 + MD5)"

def dw(s):
    """显示宽度：CJK 字符 = 2 列，其余 = 1 列"""
    w = 0
    for c in s:
        cp = ord(c)
        if (0x1100  <= cp <= 0x115F  or 0x2329  <= cp <= 0x232A
            or 0x2E80  <= cp <= 0xA4CF  or 0xA960  <= cp <= 0xA97C
            or 0xAC00  <= cp <= 0xD7A3  or 0xF900  <= cp <= 0xFAFF
            or 0xFE10  <= cp <= 0xFE19  or 0xFE30  <= cp <= 0xFE6F
            or 0xFF01  <= cp <= 0xFF60  or 0xFFE0  <= cp <= 0xFFE6
            or 0x1F300 <= cp <= 0x1F64F or 0x1F900 <= cp <= 0x1F9FF
            or 0x20000 <= cp <= 0x2FFFD or 0x30000 <= cp <= 0x3FFFD):
            w += 2
        else:
            w += 1
    return w

def pad(s, width):
    d = dw(s)
    return s + ' ' * max(0, width - d)

BOX_W = 58  # 框内宽度（═ 的数量）

lines = [
    ("总耗时",   f"{total_time} 秒"),
    ("原始文件", str(orig_count)),
    ("Shard 数", str(shard_files)),
    ("EC 保护",  ec_status),
    ("文件校验", file_check),
]
max_lw = max(dw(l) for l, _ in lines)

print()
print("╔" + "═" * BOX_W + "╗")
title = "  ✓ 全流程完成"
# 居中标题
tdw = dw(title)
left = (BOX_W - 2 - tdw) // 2
right = BOX_W - 2 - tdw - left
print("║ " + " " * left + title + " " * right + " ║")
print("╠" + "═" * BOX_W + "╣")

for label, value in lines:
    lp = pad(label, max_lw)
    content = f" {lp}:  {value}"
    cdw = dw(content)
    if cdw > BOX_W - 4:
        # 截断过长的值
        while dw(content) > BOX_W - 4 and len(content) > 0:
            content = content[:-1]
    trail = BOX_W - 2 - dw(content)
    print("║ " + content + " " * trail + " ║")

print("╚" + "═" * BOX_W + "╝")
print()
PYEOF
