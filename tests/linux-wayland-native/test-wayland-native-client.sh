#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"

echo "▶ Native client host-safe unit tests..."
GOCACHE=/tmp/llrdc-go-cache go test ./client

echo "▶ Native client Docker native/cgo unit tests..."
docker build -f "${ROOT_DIR}/Dockerfile.client" --target test -t "${IMAGE_NAME}-test" "${ROOT_DIR}"

echo "▶ Native client ULL latency benchmark..."
export WEBTRANSPORT_ENABLED="${WEBTRANSPORT_ENABLED:-true}"
export LLRDC_VIDEO_CODEC="${LLRDC_VIDEO_CODEC:-h264_nvenc}"
"${ROOT_DIR}/tests/linux-wayland-native/benchmark-wayland-native-latency.sh"

echo "Native Linux client test passed"
