#!/usr/bin/env bash
# docker-build.sh — Build the llrdc Docker image.
set -euo pipefail

IMAGE_TAG_EXPLICIT="false"

if [ -n "${IMAGE_TAG+x}" ]; then
  IMAGE_TAG_EXPLICIT="true"
fi

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
DOCKERFILE="Dockerfile"
ENABLE_INTEL="false"
ENABLE_NVIDIA="false"
ENABLE_MACOS="false"
BUILD_VARIANT="cpu"
USE_DRY_RUN="false"
NO_CACHE="false"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/scripts/docker/common.sh"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --*=*)
      KEY="${1%%=*}"
      VALUE="${1#*=}"
      shift
      set -- "$KEY" "$VALUE" "$@"
      continue
      ;;
    --intel)
      ENABLE_INTEL="true"
      BUILD_VARIANT="intel"
      shift
      ;;
    --nvidia)
      ENABLE_NVIDIA="true"
      BUILD_VARIANT="nvidia"
      shift
      ;;
    --macos)
      ENABLE_MACOS="true"
      DOCKERFILE="Dockerfile.macos"
      BUILD_VARIANT="macos"
      shift
      ;;
    --dry-run)
      USE_DRY_RUN="true"
      shift
      ;;
    --no-cache)
      NO_CACHE="true"
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

if [ "${IMAGE_TAG_EXPLICIT}" = "false" ]; then
  if [ "${ENABLE_MACOS}" = "true" ]; then
    IMAGE_TAG="macos"
  elif [ "${ENABLE_INTEL}" = "true" ]; then
    IMAGE_TAG="intel"
  elif [ "${ENABLE_NVIDIA}" = "true" ]; then
    IMAGE_TAG="nvidia"
  fi
fi

echo "▶ Building Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Dockerfile: ${DOCKERFILE}"
echo "  Context: ${SCRIPT_DIR}"
echo "  Variant: ${BUILD_VARIANT}"

DOCKER_BUILD_CMD=(
  docker build
  -f "${SCRIPT_DIR}/${DOCKERFILE}"
)

if [ "${NO_CACHE}" = "true" ]; then
  DOCKER_BUILD_CMD+=(--no-cache)
fi

DOCKER_BUILD_CMD+=(
  --build-arg "UID=$(id -u)"
  --build-arg "ENABLE_INTEL=${ENABLE_INTEL}"
  --build-arg "ENABLE_NVIDIA=${ENABLE_NVIDIA}"
  --build-arg "BUILD_VARIANT=${BUILD_VARIANT}"
  --tag "${IMAGE_NAME}:${IMAGE_TAG}"
  "${SCRIPT_DIR}"
)

if [ "$USE_DRY_RUN" = "true" ]; then
  echo "Dry run command:"
  print_command "${DOCKER_BUILD_CMD[@]}"
  exit 0
fi

"${DOCKER_BUILD_CMD[@]}"

echo "✅ Build complete: ${IMAGE_NAME}:${IMAGE_TAG}"
