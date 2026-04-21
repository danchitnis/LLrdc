#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"
SERVER_IMAGE_NAME="${SERVER_IMAGE_NAME:-danchitnis/llrdc:latest}"
NETWORK_NAME="llrdc-native-e2e"
SERVER_NAME="llrdc-native-e2e-server"
CLIENT_NAME="llrdc-native-e2e-client"
HOST_PORT="${LLRDC_CLIENT_CONTROL_PORT:-18080}"

cleanup() {
  docker rm -f "${CLIENT_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${SERVER_NAME}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"${ROOT_DIR}/docker-build.sh"
docker build -f "${ROOT_DIR}/Dockerfile.client" -t "${CLIENT_IMAGE_NAME}" "${ROOT_DIR}"

docker network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
docker network create "${NETWORK_NAME}" >/dev/null

docker run -d --rm --name "${SERVER_NAME}" --network "${NETWORK_NAME}" \
  "${SERVER_IMAGE_NAME}" /app/llrdc --port 8080 --test-pattern --video-codec vp8 >/dev/null

docker run -d --rm --name "${CLIENT_NAME}" --network host \
  -v /tmp/.X11-unix:/tmp/.X11-unix -e DISPLAY="${DISPLAY:-:0}" \
  -v "${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY:-wayland-0}:${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY:-wayland-0}" \
  -e WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}" \
  -e XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR}" \
  -e XAUTHORITY="${XAUTHORITY:-}" \
  --ipc=host \
  --entrypoint /bin/sh "${CLIENT_IMAGE_NAME}" -lc \
  "/usr/local/bin/llrdc-client --server http://127.0.0.1:8080 --control-addr 0.0.0.0:18080 --auto-start --exit-after 20s" >/dev/null

for _ in {1..80}; do
  stats="$(curl -fsS "http://127.0.0.1:${HOST_PORT}/statsz" 2>/dev/null || true)"
  if echo "${stats}" | grep -q '"presentedFrames":[1-9]'; then
    curl -fsS "http://127.0.0.1:${HOST_PORT}/snapshotz" >/tmp/llrdc-native-e2e.png
    echo "Native client E2E test passed"
    exit 0
  fi
  sleep 0.5
done

echo "Native client E2E test failed"
docker logs "${SERVER_NAME}" || true
docker logs "${CLIENT_NAME}" || true
exit 1
