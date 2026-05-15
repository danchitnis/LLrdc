#!/usr/bin/env bash
set -euo pipefail

# benchmark-intel-native.sh
# Runs the native client latency benchmark specifically for Intel QSV (H264).

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

# Overrides for Intel QSV
export LLRDC_VIDEO_CODEC="h264_qsv"

# Create a temporary benchmark script in the scripts directory to preserve path resolution
TEMP_BENCH="${ROOT_DIR}/scripts/benchmark-intel-tmp.sh"
cp "${ROOT_DIR}/tests/linux-wayland-native/benchmark-wayland-native-latency.sh" "${TEMP_BENCH}"
chmod +x "${TEMP_BENCH}"

cleanup() {
    rm -f "${TEMP_BENCH}"
}
trap cleanup EXIT

# Inject Intel-specific docker arguments into the start_server function
# 1. Change image from :latest to :intel
# 2. Add --device /dev/dri and Intel env vars
sed -i 's|danchitnis/llrdc:latest|danchitnis/llrdc:intel|g' "${TEMP_BENCH}"
sed -i '/docker run -d --name "${CONTAINER_NAME}" \\/a \    --device /dev/dri:/dev/dri \\\n    -e USE_INTEL=true \\\n    -e LIBVA_DRIVER_NAME=iHD \\' "${TEMP_BENCH}"

# Inject log saving into the TEMP_BENCH cleanup function
sed -i '/docker rm -f "${CONTAINER_NAME}"/i \  docker logs "${CONTAINER_NAME}" > /tmp/llrdc-benchmark-server.log 2>\&1 || true' "${TEMP_BENCH}"

# Inject client config to force h264
sed -i '/start_client() {/a \
  echo "codec: h264" > /tmp/llrdc-intel-client-config.yaml\n' "${TEMP_BENCH}"
sed -i 's|--auto-start \\|--auto-start \\\n    --config /tmp/llrdc-intel-client-config.yaml \\|g' "${TEMP_BENCH}"

# Clean up modified run script on exit
sed -i '/rm -f "${CONTAINER_NAME}"/i \  rm -f /tmp/llrdc-intel-client-config.yaml' "${TEMP_BENCH}"



# Also ensure it builds the intel image during the build step
sed -i 's|"${ROOT_DIR}/docker-build.sh"|"${ROOT_DIR}/docker-build.sh" --intel|g' "${TEMP_BENCH}"

# Run the modified benchmark
echo "▶ Starting Intel QSV Native Latency Benchmark..."
bash "${TEMP_BENCH}" "$@"
