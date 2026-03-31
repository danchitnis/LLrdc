#!/usr/bin/env bash
set -euo pipefail

run_profile() {
  local name="$1"
  shift
  echo "==> ${name}"
  docker rm -f llrdc-latency-breakdown-compat llrdc-latency-breakdown-direct >/dev/null 2>&1 || true
  "$@"
}

run_profile "CPU VP8 1080p30" npm run test:latency:cpu-1080p30
run_profile "CPU VP8 1080p60" npm run test:latency:cpu-1080p60
run_profile "GPU AV1 NVENC 4K60" npm run test:latency:gpu-4k60
