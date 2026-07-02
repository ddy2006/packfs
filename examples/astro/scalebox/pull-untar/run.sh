#!/usr/bin/env bash

set -eo pipefail
source /usr/local/lib/scalebox/functions.sh

body=$1
source_root=$(scalebox::task_header "$2" "source_root")
target_root=$(scalebox::task_header "$2" "target_root")
bw_limit=$(scalebox::task_header "$2" "bw_limit")

src="${source_root}/${body}"

mkdir -p "$target_root"

if [ -n "$bw_limit" ]; then
  reader="pv -L $bw_limit"
else
  reader="cat"
fi

case "$body" in
  *.tar.zst)
    $reader "$src" | zstd -d | tar -xf - -C "$target_root" || exit $?
    ;;
  *.tar)
    $reader "$src" | tar -xf - -C "$target_root" || exit $?
    ;;
  *)
    mkdir -p "$(dirname "${target_root}/${body}")"
    $reader "$src" > "${target_root}/${body}" || exit $?
    ;;
esac

echo "$body" > ${WORK_DIR}/sink-tasks.txt
exit 0
