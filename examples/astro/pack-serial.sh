#!/bin/bash
# 完整串行打包流程
#
# Usage: bash pack-serial.sh <def-file> <data-root> <output-root>
#
# 环境变量:
#   PACKFS_BIN   packfs 二进制路径（默认 packfs）
#   SQLITE_DB    数据库路径（默认 ./packfs.db）

set -e

DEF="${1:-dataset.def}"
PACKFS="${PACKFS_BIN:-packfs}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

DAT_ROOT="${DATA_ROOT:-./data}"
DAT_DIR="$DAT_ROOT/dat"
DEF_DIR="$DAT_ROOT/def"
SHARD_DIR="$DAT_ROOT/shard"
UNPACK_DIR="$DAT_ROOT/unpack"

DS_NAME=$(awk '/^  name:/{print $2}' "$DEF")
FORMAT=$(awk '/^  format:/{print $2}' "$DEF")
COMPRESS=$(awk '/^  compress:/{print $2}' "$DEF")

[ -z "$FORMAT" ] && FORMAT=bin
[ -z "$COMPRESS" ] && COMPRESS=""

export SQLITE_DB="${SQLITE_DB:-$(pwd)/packfs.db}"

mkdir -p "$DEF_DIR" "$SHARD_DIR" "$UNPACK_DIR"

echo "========================================"
echo "Dataset:   $DS_NAME"
echo "Data:      $DAT_DIR"
echo "Defs:      $DEF_DIR"
echo "Shards:    $SHARD_DIR"
echo "Unpack:    $UNPACK_DIR"
echo "Format:    $FORMAT"
echo "Compress:  ${COMPRESS:-none}"
echo "========================================"

# 检查数据目录是否存在
if [ ! -d "$DAT_DIR/$DS_NAME" ]; then
  echo "ERROR: data dir not found: $DAT_DIR/$DS_NAME" >&2
  echo "       Run 'bash simulate.sh' first." >&2
  exit 1
fi

START_TOTAL=$(date +%s)

# Step 1: dataset create
echo ""
echo ">>> Step 1: dataset create"
START=$SECONDS
"$PACKFS" dataset create --root-dir="$DAT_DIR" --name="$DS_NAME"
DS_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_dataset WHERE name='$DS_NAME'")
echo "  dataset_id=$DS_ID   time=${SECONDS}s"

# Step 2: arcset create
echo ""
echo ">>> Step 2: arcset create"
START=$SECONDS
ARGS=(--name="${DS_NAME}-arc" --target-root="$SHARD_DIR" --dataset-ids="$DS_ID" --format="$FORMAT")
[ -n "$COMPRESS" ] && ARGS+=(--compress="$COMPRESS")
"$PACKFS" arcset create "${ARGS[@]}"
ARC_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_arcset WHERE name='${DS_NAME}-arc'")
echo "  arcset_id=$ARC_ID   time=${SECONDS}s"

# Step 3: gen-def (script mode)
echo ""
echo ">>> Step 3: gen-def (script mode)"
START=$SECONDS
"$PACKFS" arcset gen-def --id="$ARC_ID" --target-root="$DEF_DIR" \
  --script="$SCRIPT_DIR/gen-def.sh"
SHARD_COUNT=$(sqlite3 "$SQLITE_DB" \
  "SELECT json_extract(metadata, '$.shard_count') FROM t_arcset WHERE id=$ARC_ID")
echo "  shard_count=$SHARD_COUNT   time=${SECONDS}s"

# Step 4: shard make (serial)
echo ""
echo ">>> Step 4: shard make ($SHARD_COUNT shards)"
START=$SECONDS
i=0
for def in "$DEF_DIR"/*.def; do
  [ -f "$def" ] || continue
  i=$((i + 1))
  echo "  [$i/$SHARD_COUNT] $(basename "$def")"
  "$PACKFS" shard make --def-file="$def" 2>&1 | grep -v "level=" || true
done
echo "  done: $i shards   time=${SECONDS}s"

# Step 5: arcset validate
echo ""
echo ">>> Step 5: arcset validate"
START=$SECONDS
"$PACKFS" arcset validate --id="$ARC_ID"
echo "  time=${SECONDS}s"

# Step 6: arcset finalize
echo ""
echo ">>> Step 6: arcset finalize"
START=$SECONDS
"$PACKFS" arcset finalize --id="$ARC_ID"
echo "  time=${SECONDS}s"

# Step 7: checksum spot-check
echo ""
echo ">>> Step 7: checksum spot-check"
START=$SECONDS
sample_shard=$(sqlite3 "$SQLITE_DB" \
  "SELECT file_path FROM t_shard WHERE arcset=$ARC_ID ORDER BY seq LIMIT 1")
db_sum=$(sqlite3 "$SQLITE_DB" \
  "SELECT sha256 FROM t_shard WHERE arcset=$ARC_ID AND file_path='$sample_shard'")
disk_sum=$(shasum -a 256 "$SHARD_DIR/$sample_shard" | awk '{print $1}')
if [ "$db_sum" = "$disk_sum" ]; then
  echo "  checksum OK: $sample_shard   time=${SECONDS}s"
else
  echo "  checksum MISMATCH: $sample_shard (db=$db_sum, disk=$disk_sum)" >&2
  exit 1
fi

# Step 8: unpack test
echo ""
echo ">>> Step 8: unpack test"
START=$SECONDS
rm -rf "$UNPACK_DIR"
mkdir -p "$UNPACK_DIR"

shopt -s nullglob
shards=( "$SHARD_DIR"/*.tar.zst "$SHARD_DIR"/*.bin )
shopt -u nullglob

for shard in "${shards[@]}"; do
  echo "  unpacking $(basename "$shard") ..."
  "$PACKFS" shard unpack --shard-file="$shard" --target-root="$UNPACK_DIR" 2>&1 \
    | grep -v "level=" || true
done
echo "  time=${SECONDS}s"

# Verify file count
orig_count=$(find "$DAT_DIR/$DS_NAME" -type f | wc -l | tr -d ' ')
unpack_count=$(find "$UNPACK_DIR" -type f | wc -l | tr -d ' ')
echo ""
echo "========================================"
echo "  Original files:   $orig_count"
echo "  Unpacked files:   $unpack_count"
if [ "$orig_count" -eq "$unpack_count" ]; then
  echo "  VERIFY:  OK"
else
  echo "  VERIFY:  MISMATCH" >&2
  exit 1
fi

TOTAL_TIME=$(($(date +%s) - START_TOTAL))
echo "  Total time: ${TOTAL_TIME}s"
echo "========================================"
