#!/usr/bin/env bash
# docker-run-macos.sh — Run the llrdc Docker container in macOS split mode.
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="macos"
CONTAINER_NAME="llrdc-macos"

echo "▶ Running Docker agent for macOS architecture..."
echo "  Container: ${CONTAINER_NAME}"
echo "  Image: ${IMAGE_NAME}:${IMAGE_TAG}"

docker run --rm -it \
  --name "${CONTAINER_NAME}" \
  --shm-size=2gb \
  -e PULSE_SERVER=unix:/tmp/pulseaudio.socket \
  -e USE_DEBUG_INPUT=true \
  -p 12346:12346 \
  -p 12348:12348 \
  --add-host host.docker.internal:host-gateway \
  "${IMAGE_NAME}:${IMAGE_TAG}" "$@"
