#!/usr/bin/env bash
# docker-build.sh — Build the llrdc Docker image.
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
DOCKERFILE="Dockerfile"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      if [ -n "${2:-}" ]; then
        IMAGE_TAG="$2"
        shift 2
      else
        echo "Error: --tag requires an argument."
        exit 1
      fi
      ;;
    *)
      shift
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "▶ Building Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Dockerfile: ${DOCKERFILE}"
echo "  Context: ${SCRIPT_DIR}"

docker build \
  -f "${DOCKERFILE}" \
  --build-arg UID=$(id -u) \
  --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
  "${SCRIPT_DIR}"

echo "✅ Build complete: ${IMAGE_NAME}:${IMAGE_TAG}"
