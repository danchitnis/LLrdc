#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"

echo "▶ Native client host-safe unit tests..."
GOCACHE=/tmp/llrdc-go-cache go test ./internal/client ./cmd/client

echo "▶ Native client Docker native/cgo unit tests..."
docker build -f "${ROOT_DIR}/Dockerfile.client" --target test -t "${IMAGE_NAME}-test" "${ROOT_DIR}"

echo "▶ Native client ULL latency benchmark..."
export WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-true}"
export WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-0}"
export LLRDC_VIDEO_CODEC="${LLRDC_VIDEO_CODEC:-vp8}"
"${ROOT_DIR}/scripts/benchmark-native-latency.sh"

echo "Native Linux client test passed"
