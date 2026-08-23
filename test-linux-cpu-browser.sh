#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${ROOT_DIR}/scripts/playwright-browser-env.sh"

if [[ $# -gt 0 ]]; then
    echo "Usage: $0" >&2
    echo "The CPU runner owns one connection smoke test; pass no Playwright spec arguments." >&2
    exit 2
fi

require_playwright_runtime
command -v docker >/dev/null 2>&1 || { echo "docker was not found." >&2; exit 1; }
configure_installed_chrome

CONTAINER_NAME="llrdc-wayland-test"
PORT="8081"
CONTAINER_IMAGE="${CONTAINER_IMAGE:-danchitnis/llrdc:latest}"
IMAGE_NAME="${CONTAINER_IMAGE%:*}"
IMAGE_TAG="${CONTAINER_IMAGE##*:}"
if [[ "${IMAGE_NAME}" == "${CONTAINER_IMAGE}" ]]; then
    IMAGE_TAG="latest"
fi

cleanup() {
    docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
IMAGE_NAME="${IMAGE_NAME}" IMAGE_TAG="${IMAGE_TAG}" "${ROOT_DIR}/docker-build.sh"
PORT="${PORT}" VBR=false "${ROOT_DIR}/docker-run.sh" \
    --detach --name "${CONTAINER_NAME}" --host-net

npx playwright test tests/linux-cpu-browser/wayland_minimal.spec.ts \
    --workers=1 --reporter=line --timeout=60000
