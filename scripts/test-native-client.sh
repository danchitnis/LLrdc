#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"
CONTROL_ADDR="${LLRDC_CLIENT_CONTROL_ADDR:-127.0.0.1:18080}"
HOST_PORT="${LLRDC_CLIENT_CONTROL_PORT:-18080}"
CONTAINER_NAME="llrdc-native-client-smoke"

GOCACHE=/tmp/llrdc-go-cache go test ./internal/client ./cmd/client
docker build -f "${ROOT_DIR}/Dockerfile.client" --target test -t "${IMAGE_NAME}-test" "${ROOT_DIR}"
docker build -f "${ROOT_DIR}/Dockerfile.client" -t "${IMAGE_NAME}" "${ROOT_DIR}"

cleanup() {
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
docker run -d --rm --name "${CONTAINER_NAME}" -p "${HOST_PORT}:18080" \
  --entrypoint /bin/sh "${IMAGE_NAME}" -lc \
  "xvfb-run -a /usr/local/bin/llrdc-client --control-addr 0.0.0.0:18080 --auto-start --exit-after 10s" >/dev/null

for _ in {1..40}; do
  if curl -fsS "http://${CONTROL_ADDR}/readyz" >/dev/null 2>&1; then
    curl -fsS "http://${CONTROL_ADDR}/menuz" >/dev/null
    curl -fsS "http://${CONTROL_ADDR}/snapshotz" >/tmp/llrdc-native-smoke.png
    echo "Native client smoke test passed"
    exit 0
  fi
  sleep 0.25
done

echo "Native client smoke test failed"
docker logs "${CONTAINER_NAME}" || true
exit 1
