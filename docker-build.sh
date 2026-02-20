#!/usr/bin/env bash
# docker-build.sh — Build the remote-desktop Docker image.
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-remote-desktop}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "▶ Building Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Context: ${SCRIPT_DIR}"

docker build \
  --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
  "${SCRIPT_DIR}"

echo "✅ Build complete: ${IMAGE_NAME}:${IMAGE_TAG}"
