#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

echo "client:test:e2e now uses the native latency benchmark as the authoritative Linux native E2E test."
export WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-true}"
export WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-0}"
export LLRDC_VIDEO_CODEC="${LLRDC_VIDEO_CODEC:-vp8}"
"${ROOT_DIR}/scripts/benchmark-native-latency.sh"
