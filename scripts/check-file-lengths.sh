#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_SOFT_LIMIT="${GO_SOFT_LIMIT:-500}"
GO_HARD_LIMIT="${GO_HARD_LIMIT:-700}"
TS_SOFT_LIMIT="${TS_SOFT_LIMIT:-400}"
TS_HARD_LIMIT="${TS_HARD_LIMIT:-600}"

failures=0

check_files() {
  local label="$1"
  local soft_limit="$2"
  local hard_limit="$3"
  shift 3

  local file lines rel
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    lines=$(wc -l < "$file")
    rel="${file#$ROOT_DIR/}"
    if [ "$lines" -gt "$hard_limit" ]; then
      echo "ERROR: ${rel} has ${lines} lines; ${label} hard limit is ${hard_limit}."
      failures=$((failures + 1))
    elif [ "$lines" -gt "$soft_limit" ]; then
      echo "WARN: ${rel} has ${lines} lines; ${label} soft limit is ${soft_limit}."
    fi
  done < <("$@")
}

check_files "Go" "$GO_SOFT_LIMIT" "$GO_HARD_LIMIT" \
  find "$ROOT_DIR" \
    -path "$ROOT_DIR/node_modules" -prune -o \
    -path "$ROOT_DIR/dist" -prune -o \
    -path "$ROOT_DIR/public/assets" -prune -o \
    -name '*_test.go' -prune -o \
    -type f -name '*.go' -print

check_files "TypeScript" "$TS_SOFT_LIMIT" "$TS_HARD_LIMIT" \
  find "$ROOT_DIR/src" \
    -type f \( -name '*.ts' -o -name '*.tsx' \) -print

if [ "$failures" -gt 0 ]; then
  exit 1
fi
