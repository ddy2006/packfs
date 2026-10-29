#!/bin/bash
# astro 全流程验收（新 CLI）
#
#   simulate -> dataset create -> gen-def -> shard make -> dataset mark-built
#   -> dataset unpack -> [arcset create -> append -> build-data -> make-ec
#   -> validate -> export -> recover]
#
# Usage:
#   bash full-pipeline.sh
#   bash full-pipeline.sh --with-ec
#   bash full-pipeline.sh --with-ec --ec=4+2
#   bash full-pipeline.sh --clean
#
# 环境变量:
#   PACKFS_BIN          packfs 二进制（默认 packfs）
#   PACKFS_PG_URL       PostgreSQL DSN（EC 流程必需）
#   DATA_ROOT           数据根目录（默认 ./data）
#   SHARD_MAX_BYTES     dataset shard 上限（默认 10MiB）
#   PARTITION_CAPACITY  arcset 单分区容量（默认 10GB）
#   SKIP_SIMULATE       非空则跳过 simulate

set -e

WITH_EC=""
EC_CONFIG="4+2"
CLEAN_ONLY=""
DEF_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-ec) WITH_EC=1; shift ;;
    --ec) EC_CONFIG="$2"; WITH_EC=1; shift 2 ;;
    --ec=*) EC_CONFIG="${1#*=}"; WITH_EC=1; shift ;;
    --clean) CLEAN_ONLY=1; shift ;;
    --def) DEF_FILE="$2"; shift 2 ;;
    --def=*) DEF_FILE="${1#*=}"; shift ;;
    -*) echo "Unknown option: $1" >&2; exit 1 ;;
    *) DEF_FILE="$1"; shift ;;
  esac
done

PACKFS="${PACKFS_BIN:-packfs}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEF_FILE="${DEF_FILE:-$SCRIPT_DIR/dataset.def}"
DATA_ROOT="${DATA_ROOT:-$(pwd)/data}"
DAT_DIR="$DATA_ROOT/dat"
DEF_DIR="$DATA_ROOT/def"
SHARD_DIR="$DATA_ROOT/shard"
DB_DIR="$DATA_ROOT/db"
UNPACK_DIR="$DATA_ROOT/unpack"
ARC_DIR="$DATA_ROOT/arcset"
SHARD_MAX_BYTES="${SHARD_MAX_BYTES:-10485760}"
PARTITION_CAPACITY="${PARTITION_CAPACITY:-10GB}"

DS_NAME=$(awk '/^  name:/{print $2}' "$DEF_FILE")
FORMAT=$(awk '/^  format:/{print $2}' "$DEF_FILE")
COMPRESS=$(awk '/^  compress:/{print $2}' "$DEF_FILE")
[ -z "$FORMAT" ] && FORMAT=tar
[ -z "$COMPRESS" ] && COMPRESS=""

do_clean() {
  rm -rf "$DATA_ROOT" "$DB_DIR"
  echo "cleaned: $DATA_ROOT, $DB_DIR"
}

if [ -n "$CLEAN_ONLY" ]; then
  do_clean
  exit 0
fi

if [ -n "$WITH_EC" ] && [ -z "$PACKFS_PG_URL" ]; then
  echo "ERROR: PACKFS_PG_URL is required for --with-ec" >&2
  exit 1
fi

do_clean
mkdir -p "$DAT_DIR" "$DEF_DIR" "$SHARD_DIR" "$DB_DIR" "$UNPACK_DIR"
[ -n "$WITH_EC" ] && mkdir -p "$ARC_DIR"

START_TOTAL=$(date +%s)

if [ -z "$SKIP_SIMULATE" ]; then
  echo ">>> simulate"
  DATA_ROOT="$DATA_ROOT" bash "$SCRIPT_DIR/simulate.sh" "$DEF_FILE"
fi

echo ">>> dataset create"
DS_JSON=$("$PACKFS" dataset create \
  --root-dir="$DAT_DIR" --name="$DS_NAME" \
  --db-dir="$DB_DIR" --target-root="$SHARD_DIR" \
  --format="$FORMAT" --compress="$COMPRESS" \
  --shard-max-bytes="$SHARD_MAX_BYTES" --gen-only)
DS_UID=$(printf '%s' "$DS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset_uid"])')
DS_DB=$(printf '%s' "$DS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sqlite_path"])')
echo "  uid=$DS_UID db=$DS_DB"

echo ">>> gen-def"
SHARD_COUNT=$(bash "$SCRIPT_DIR/gen-def.sh" --dataset-db="$DS_DB" --target-root="$DEF_DIR")
echo "  shards=$SHARD_COUNT"

echo ">>> shard make"
i=0
for def in "$DEF_DIR"/*.def; do
  [ -f "$def" ] || continue
  i=$((i + 1))
  "$PACKFS" shard make --def-file="$def" --output-dir="$SHARD_DIR" >/dev/null
done
echo "  built=$i"

echo ">>> dataset mark-built"
"$PACKFS" dataset mark-built --dataset-db="$DS_DB"

echo ">>> dataset unpack (before arcset move)"
"$PACKFS" dataset unpack --dataset-db="$DS_DB" \
  --source-root="$SHARD_DIR" --target-root="$UNPACK_DIR"

orig_count=$(find "$DAT_DIR/$DS_NAME" -type f | wc -l | tr -d ' ')
unpack_count=$(find "$UNPACK_DIR" -type f | wc -l | tr -d ' ')
if [ "$orig_count" -ne "$unpack_count" ]; then
  echo "ERROR: unpack count mismatch ($orig_count != $unpack_count)" >&2
  exit 1
fi
echo "  files=$unpack_count"

if [ -n "$WITH_EC" ]; then
  echo ">>> arcset create"
  AS_JSON=$("$PACKFS" arcset create \
    --name="${DS_NAME}-ec-$(date +%s)" --target-root="$ARC_DIR" \
    --ec="$EC_CONFIG" --groups=1 --layout=raid3 \
    --partition-capacity="$PARTITION_CAPACITY" \
    --shard-max-bytes="$SHARD_MAX_BYTES")
  AS_ID=$(printf '%s' "$AS_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["arcset_id"])')
  echo "  arcset_id=$AS_ID"

  echo ">>> arcset append"
  "$PACKFS" arcset append --id="$AS_ID" --dataset-db="$DS_DB"

  echo ">>> arcset build-data"
  "$PACKFS" arcset build-data --id="$AS_ID"

  echo ">>> arcset make-ec"
  "$PACKFS" arcset make-ec --id="$AS_ID"

  echo ">>> arcset validate"
  "$PACKFS" arcset validate --id="$AS_ID" --stage=all

  echo ">>> arcset export"
  "$PACKFS" arcset export --id="$AS_ID" --target-root="$ARC_DIR"

  echo ">>> recover test"
  LOST=$(python3 - "$ARC_DIR/arcset.json" <<'PY'
import json,sys
m=json.load(open(sys.argv[1]))
for s in m["shards"]:
    if s["type"] == "DATA":
        print(s["name"], s["sha256"])
        break
PY
)
  LOST_PATH=$(printf '%s' "$LOST" | awk '{print $1}')
  LOST_SUM=$(printf '%s' "$LOST" | awk '{print $2}')
  rm -f "$ARC_DIR/$LOST_PATH"
  "$PACKFS" arcset recover --id="$AS_ID" --shard-file="$LOST_PATH" --target=all
  NEW_SUM=$(shasum -a 256 "$ARC_DIR/$LOST_PATH" | awk '{print $1}')
  if [ "$LOST_SUM" != "$NEW_SUM" ]; then
    echo "ERROR: recovered checksum mismatch" >&2
    exit 1
  fi
  echo "  recovered=$LOST_PATH"

  "$PACKFS" arcset validate --id="$AS_ID" --stage=all
fi

TOTAL_TIME=$(($(date +%s) - START_TOTAL))
echo "========================================"
echo "  files: $orig_count"
echo "  shards: $SHARD_COUNT"
[ -n "$WITH_EC" ] && echo "  ec: $EC_CONFIG"
echo "  time: ${TOTAL_TIME}s"
echo "  VERIFY: OK"
echo "========================================"
