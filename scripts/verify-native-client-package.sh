#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_ROOT="${PACKAGE_ROOT:-${ROOT_DIR}/dist}"
PACKAGE_NAME="${PACKAGE_NAME:-llrdc-client-linux-amd64}"
CLIENT_BIN="${PACKAGE_ROOT}/${PACKAGE_NAME}/bin/llrdc-client"
CONTROL_ADDR="${LLRDC_CLIENT_CONTROL_ADDR:-127.0.0.1:18080}"

if [[ ! -x "${CLIENT_BIN}" ]]; then
  "${ROOT_DIR}/scripts/package-native-client.sh"
fi

cleanup() {
  if [[ -n "${CLIENT_PID:-}" ]]; then
    kill "${CLIENT_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

"${CLIENT_BIN}" --headless --control-addr "${CONTROL_ADDR}" --exit-after 3s >/tmp/llrdc-verify-package.log 2>&1 &
CLIENT_PID=$!

for _ in {1..20}; do
  if curl -fsS "http://${CONTROL_ADDR}/readyz" >/dev/null 2>&1; then
    wait "${CLIENT_PID}"
    echo "Verified packaged client runtime via control API on ${CONTROL_ADDR}"
    exit 0
  fi
  sleep 0.25
done

echo "Packaged client failed to expose the control API"
cat /tmp/llrdc-verify-package.log
exit 1
