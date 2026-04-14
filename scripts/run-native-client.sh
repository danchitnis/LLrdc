#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_ROOT="${PACKAGE_ROOT:-${ROOT_DIR}/dist}"
PACKAGE_NAME="${PACKAGE_NAME:-llrdc-client-linux-amd64}"
PACKAGE_DIR="${PACKAGE_ROOT}/${PACKAGE_NAME}"
CLIENT_BIN="${PACKAGE_DIR}/bin/llrdc-client"

SERVER_URL="${LLRDC_CLIENT_SERVER:-http://127.0.0.1:8080}"
CONTROL_ADDR="${LLRDC_CLIENT_CONTROL_ADDR:-127.0.0.1:18080}"
WINDOW_WIDTH="${LLRDC_CLIENT_WIDTH:-1280}"
WINDOW_HEIGHT="${LLRDC_CLIENT_HEIGHT:-720}"
WINDOW_TITLE="${LLRDC_CLIENT_TITLE:-LLrdc Native Client}"
REBUILD=1

package_is_stale() {
  [[ ! -x "${CLIENT_BIN}" ]] && return 0
  find \
    "${ROOT_DIR}/cmd" \
    "${ROOT_DIR}/internal" \
    "${ROOT_DIR}/Dockerfile.client" \
    "${ROOT_DIR}/go.mod" \
    "${ROOT_DIR}/go.sum" \
    -newer "${CLIENT_BIN}" \
    -print -quit | grep -q .
}

usage() {
  cat <<EOF
Usage: ./scripts/run-native-client.sh [--rebuild] [--] [extra client flags...]

Environment overrides:
  LLRDC_CLIENT_SERVER=http://127.0.0.1:8080
  LLRDC_CLIENT_CONTROL_ADDR=127.0.0.1:18080
  LLRDC_CLIENT_WIDTH=1280
  LLRDC_CLIENT_HEIGHT=720
  LLRDC_CLIENT_TITLE="LLrdc Native Client"
  SDL_VIDEODRIVER=x11|wayland
  PACKAGE_NAME=llrdc-client-linux-amd64
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rebuild)
      REBUILD=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

if [[ ${REBUILD} -eq 1 || ! -x "${CLIENT_BIN}" ]] || package_is_stale; then
  "${ROOT_DIR}/scripts/package-native-client.sh"
fi

exec "${CLIENT_BIN}" \
  --server "${SERVER_URL}" \
  --control-addr "${CONTROL_ADDR}" \
  --width "${WINDOW_WIDTH}" \
  --height "${WINDOW_HEIGHT}" \
  --title "${WINDOW_TITLE}" \
  "$@"
