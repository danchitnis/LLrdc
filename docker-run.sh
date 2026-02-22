#!/usr/bin/env bash
# docker-run.sh — Run the llrdc Docker container.
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="${CONTAINER_NAME:-llrdc}"

# Port mappings (override via env vars)
HOST_PORT="${HOST_PORT:-8080}"
CONTAINER_PORT="${CONTAINER_PORT:-8080}"

# Optional: override server env vars by setting them before calling this script.
#   PORT=9090 HOST_PORT=9090 ./docker-run.sh
SERVER_PORT="${PORT:-8080}"
SERVER_FPS="${FPS:-30}"
SERVER_DISPLAY_NUM="${DISPLAY_NUM:-99}"
SERVER_VIDEO_CODEC="${VIDEO_CODEC:-h264}"

# Detect number of CPUs for maximum throughput
NUM_CPUS=$(nproc)
CPU_LIST="0-$((NUM_CPUS - 1))"


echo "▶ Starting container: ${CONTAINER_NAME}"
echo "  Image : ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Port  : ${HOST_PORT} → ${CONTAINER_PORT}"
echo "  CPUs  : ${NUM_CPUS} (cores ${CPU_LIST})"

docker run \
  --rm \
  --interactive \
  --tty \
  --name "${CONTAINER_NAME}" \
  --publish "${HOST_PORT}:${CONTAINER_PORT}" \
  --shm-size 256m \
  --cpuset-cpus "${CPU_LIST}" \
  --ulimit rtprio=99 \
  --cap-add=SYS_NICE \
  --env PORT="${SERVER_PORT}" \
  --env FPS="${SERVER_FPS}" \
  --env DISPLAY_NUM="${SERVER_DISPLAY_NUM}" \
  --env VIDEO_CODEC="${SERVER_VIDEO_CODEC}" \
  "${IMAGE_NAME}:${IMAGE_TAG}"
