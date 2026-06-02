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
DATA_ROOT="${2:-./shard-data}"
OUTPUT_ROOT="${3:-./output}"
PACKFS="${PACKFS_BIN:-packfs}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

DS_NAME=$(awk '/^  name:/{print $2}' "$DEF")
FORMAT=$(awk '/^  format:/{print $2}' "$DEF")
COMPRESS=$(awk '/^  compress:/{print $2}' "$DEF")

[ -z "$FORMAT" ] && FORMAT=bin
[ -z "$COMPRESS" ] && COMPRESS=""

export SQLITE_DB="${SQLITE_DB:-$(pwd)/packfs.db}"

echo "========================================"
echo "Dataset:  $DS_NAME"
echo "Data:     $DATA_ROOT/$DS_NAME"
echo "Output:   $OUTPUT_ROOT"
echo "Format:   $FORMAT"
echo "Compress: ${COMPRESS:-none}"
echo "DB:       $SQLITE_DB"
echo "========================================"

# Step 1: dataset create
echo ""
echo ">>> Step 1: dataset create"
"$PACKFS" dataset create --root-dir="$DATA_ROOT" --name="$DS_NAME"
DS_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_dataset WHERE name='$DS_NAME'")
echo "  dataset_id=$DS_ID"

# Step 2: arcset create
echo ""
echo ">>> Step 2: arcset create"
ARGS=(--name="${DS_NAME}-arc" --target-root="$OUTPUT_ROOT" --dataset-ids="$DS_ID" --format="$FORMAT")
[ -n "$COMPRESS" ] && ARGS+=(--compress="$COMPRESS")
"$PACKFS" arcset create "${ARGS[@]}"
ARC_ID=$(sqlite3 "$SQLITE_DB" "SELECT id FROM t_arcset WHERE name='${DS_NAME}-arc'")
echo "  arcset_id=$ARC_ID"

# Step 3: gen-def (script mode)
echo ""
echo ">>> Step 3: gen-def (script mode)"
"$PACKFS" arcset gen-def --id="$ARC_ID" --target-root="$OUTPUT_ROOT" \
  --script="$SCRIPT_DIR/gen-def.sh" --dataset-id "$DS_ID"
SHARD_COUNT=$(sqlite3 "$SQLITE_DB" \
  "SELECT json_extract(metadata, '$.shard_count') FROM t_arcset WHERE id=$ARC_ID")
echo "  shard_count=$SHARD_COUNT"

# Step 4: shard make (serial)
echo ""
echo ">>> Step 4: shard make ($SHARD_COUNT shards)"
i=0
for def in "$OUTPUT_ROOT"/*.def; do
  [ -f "$def" ] || continue
  i=$((i + 1))
  echo "  [$i/$SHARD_COUNT] $def"
  "$PACKFS" shard make --def-file="$def" 2>&1 | grep -v "level=" || true
done
echo "  done: $i shards created"

# Step 5: arcset validate
echo ""
echo ">>> Step 5: arcset validate"
"$PACKFS" arcset validate --id="$ARC_ID"
echo "  valid: OK"

# Step 6: arcset finalize
echo ""
echo ">>> Step 6: arcset finalize"
"$PACKFS" arcset finalize --id="$ARC_ID"
echo "  finalize: OK"

# Step 7: checksum spot-check
echo ""
echo ">>> Step 7: checksum spot-check"
# 从 DB 取第一个 shard 的 checksum，与文件重算对比
sample_shard=$(sqlite3 "$SQLITE_DB" \
  "SELECT file_path FROM t_shard WHERE arcset=$ARC_ID ORDER BY seq LIMIT 1")
db_sum=$(sqlite3 "$SQLITE_DB" \
  "SELECT sha256 FROM t_shard WHERE arcset=$ARC_ID AND file_path='$sample_shard'")
disk_sum=$(shasum -a 256 "$OUTPUT_ROOT/$sample_shard" | awk '{print $1}')
if [ "$db_sum" = "$disk_sum" ]; then
  echo "  checksum OK: $sample_shard"
else
  echo "  checksum MISMATCH: $sample_shard (db=$db_sum, disk=$disk_sum)" >&2
  exit 1
fi

# Step 8: unpack test
echo ""
echo ">>> Step 8: unpack test"
UNPACK_DIR="$OUTPUT_ROOT/_unpack"
rm -rf "$UNPACK_DIR"
mkdir -p "$UNPACK_DIR"

shopt -s nullglob
shards=( "$OUTPUT_ROOT"/*.tar.zst "$OUTPUT_ROOT"/*.bin )
shopt -u nullglob

for shard in "${shards[@]}"; do
  echo "  unpacking $(basename "$shard") ..."
  "$PACKFS" shard unpack --shard-file="$shard" --target-root="$UNPACK_DIR" 2>&1 \
    | grep -v "level=" || true
done

# Verify file count
orig_count=$(find "$DATA_ROOT/$DS_NAME" -type f | wc -l | tr -d ' ')
unpack_count=$(find "$UNPACK_DIR" -type f | wc -l | tr -d ' ')
echo ""
echo "========================================"
echo "  Original files:   $orig_count"
echo "  Unpacked files:   $unpack_count"
if [ "$orig_count" -eq "$unpack_count" ]; then
  echo "  VERIFY:  OK"
  echo "========================================"
else
  echo "  VERIFY:  MISMATCH" >&2
  echo "========================================"
  exit 1
fi
