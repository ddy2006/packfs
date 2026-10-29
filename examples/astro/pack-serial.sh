#!/bin/bash
# 完整串行打包流程（新 CLI：per-dataset SQLite）
#
# Usage: bash pack-serial.sh <def-file>
#
# 环境变量:
#   PACKFS_BIN           packfs 二进制路径（默认 packfs）
#   DATA_ROOT            数据根目录（默认 ./data）
#   SHARD_MAX_BYTES      dataset shard 上限（默认 10MiB）

set -e

DEF="${1:-dataset.def}"
PACKFS="${PACKFS_BIN:-packfs}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

DAT_ROOT="${DATA_ROOT:-./data}"
DAT_DIR="$DAT_ROOT/dat"
DEF_DIR="$DAT_ROOT/def"
SHARD_DIR="$DAT_ROOT/shard"
DB_DIR="$DAT_ROOT/db"
UNPACK_DIR="$DAT_ROOT/unpack"
SHARD_MAX_BYTES="${SHARD_MAX_BYTES:-10485760}"

DS_NAME=$(awk '/^  name:/{print $2}' "$DEF")
FORMAT=$(awk '/^  format:/{print $2}' "$DEF")
COMPRESS=$(awk '/^  compress:/{print $2}' "$DEF")
[ -z "$FORMAT" ] && FORMAT=bin
[ -z "$COMPRESS" ] && COMPRESS=""

rm -rf "$DEF_DIR" "$SHARD_DIR" "$DB_DIR" "$UNPACK_DIR"
mkdir -p "$DEF_DIR" "$SHARD_DIR" "$DB_DIR" "$UNPACK_DIR"

echo "========================================"
echo "Dataset:   $DS_NAME"
echo "Data:      $DAT_DIR"
echo "Defs:      $DEF_DIR"
echo "Shards:    $SHARD_DIR"
echo "DB:        $DB_DIR"
echo "Unpack:    $UNPACK_DIR"
echo "Format:    $FORMAT"
echo "Compress:  ${COMPRESS:-none}"
echo "========================================"

if [ ! -d "$DAT_DIR/$DS_NAME" ]; then
  echo "ERROR: data dir not found: $DAT_DIR/$DS_NAME" >&2
  echo "       Run 'bash simulate.sh' first." >&2
  exit 1
fi

START_TOTAL=$(date +%s)

# Step 1: dataset create（扫描目录 + 写独立 SQLite）
echo ""
echo ">>> Step 1: dataset create"
START=$SECONDS
DS_JSON=$("$PACKFS" dataset create \
  --root-dir="$DAT_DIR" \
  --name="$DS_NAME" \
  --db-dir="$DB_DIR" \
  --target-root="$SHARD_DIR" \
  --format="$FORMAT" \
  --compress="$COMPRESS" \
  --shard-max-bytes="$SHARD_MAX_BYTES" \
  --gen-only)
DS_UID=$(printf '%s' "$DS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset_uid"])')
DS_DB=$(printf '%s' "$DS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sqlite_path"])')
echo "  dataset_uid=$DS_UID"
echo "  sqlite=$DS_DB"
echo "  time=${SECONDS}s"

# Step 2: gen-def（按 channel 分组）
echo ""
echo ">>> Step 2: gen-def (script mode)"
START=$SECONDS
SHARD_COUNT=$(bash "$SCRIPT_DIR/gen-def.sh" --dataset-db="$DS_DB" --target-root="$DEF_DIR")
echo "  shard_count=$SHARD_COUNT   time=${SECONDS}s"

# Step 3: shard make（串行，指定输出目录）
echo ""
echo ">>> Step 3: shard make ($SHARD_COUNT shards)"
START=$SECONDS
i=0
for def in "$DEF_DIR"/*.def; do
  [ -f "$def" ] || continue
  i=$((i + 1))
  echo "  [$i/$SHARD_COUNT] $(basename "$def")"
  "$PACKFS" shard make --def-file="$def" --output-dir="$SHARD_DIR" >/dev/null
done
echo "  done: $i shards   time=${SECONDS}s"

# Step 4: 标记 dataset built（校验所有 DATA shard）
echo ""
echo ">>> Step 4: dataset mark-built"
START=$SECONDS
"$PACKFS" dataset mark-built --dataset-db="$DS_DB"
echo "  time=${SECONDS}s"

# Step 5: checksum spot-check
echo ""
echo ">>> Step 5: checksum spot-check"
START=$SECONDS
sample_shard=$(sqlite3 "$DS_DB" \
  "SELECT file_path FROM t_shard ORDER BY seq LIMIT 1")
db_sum=$(sqlite3 "$DS_DB" \
  "SELECT sha256 FROM t_shard WHERE file_path='$sample_shard'")
disk_sum=$(shasum -a 256 "$SHARD_DIR/$sample_shard" | awk '{print $1}')
if [ "$db_sum" = "$disk_sum" ]; then
  echo "  checksum OK: $sample_shard   time=${SECONDS}s"
else
  echo "  checksum MISMATCH: $sample_shard (db=$db_sum, disk=$disk_sum)" >&2
  exit 1
fi

# Step 6: unpack test
echo ""
echo ">>> Step 6: dataset unpack"
START=$SECONDS
"$PACKFS" dataset unpack \
  --dataset-db="$DS_DB" \
  --source-root="$SHARD_DIR" \
  --target-root="$UNPACK_DIR"
echo "  time=${SECONDS}s"

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
