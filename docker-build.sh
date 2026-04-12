#!/usr/bin/env bash
# docker-build.sh — Build the llrdc Docker image.
set -euo pipefail

IMAGE_TAG_EXPLICIT="false"

if [[ -v IMAGE_TAG ]]; then
  IMAGE_TAG_EXPLICIT="true"
fi

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
DOCKERFILE="Dockerfile"
ENABLE_INTEL="false"
BUILD_VARIANT="cpu"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --intel)
      ENABLE_INTEL="true"
      BUILD_VARIANT="intel"
      shift
      ;;
    --tag)
      if [ -n "${2:-}" ]; then
        IMAGE_TAG="$2"
        IMAGE_TAG_EXPLICIT="true"
        shift 2
      else
        echo "Error: --tag requires an argument."
        exit 1
      fi
      ;;
    *)
      echo "Error: Unknown argument: $1"
      exit 1
      ;;
  esac
done

if [ "${ENABLE_INTEL}" = "true" ] && [ "${IMAGE_TAG_EXPLICIT}" = "false" ]; then
  IMAGE_TAG="intel"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "▶ Building Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Dockerfile: ${DOCKERFILE}"
echo "  Context: ${SCRIPT_DIR}"
echo "  Variant: ${BUILD_VARIANT}"

docker build \
  -f "${DOCKERFILE}" \
  --build-arg UID=$(id -u) \
  --build-arg ENABLE_INTEL="${ENABLE_INTEL}" \
  --build-arg BUILD_VARIANT="${BUILD_VARIANT}" \
  --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
  "${SCRIPT_DIR}"

echo "✅ Build complete: ${IMAGE_NAME}:${IMAGE_TAG}"
