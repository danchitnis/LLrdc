#!/usr/bin/env bash
# docker-run.sh — Run the llrdc Docker container.
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="${CONTAINER_NAME:-llrdc}"

SERVER_PORT="${PORT:-8080}"
SERVER_FPS="${FPS:-30}"
SERVER_DISPLAY_NUM="${DISPLAY_NUM:-99}"
SERVER_VIDEO_CODEC="${VIDEO_CODEC:-h264}"

# Port mappings (override via env vars)
HOST_PORT="${HOST_PORT:-8080}"
CONTAINER_PORT="${CONTAINER_PORT:-$SERVER_PORT}"

USE_GPU="${USE_GPU:-false}"
for arg in "$@"; do
  if [ "$arg" == "--gpu" ]; then
    USE_GPU="true"
  fi
done

GPU_ARGS=""
if [ "$USE_GPU" = "true" ]; then
  # Verify if Docker has NVIDIA runtime/toolkit support
  if ! docker info 2>/dev/null | grep -qi "Runtimes.*nvidia"; then
    if ! docker info 2>/dev/null | grep -qi "nvidia"; then
      echo "❌ ERROR: Docker does not appear to support NVIDIA GPUs."
      echo "Please install the NVIDIA Container Toolkit and restart Docker."
      echo "  https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html"
      echo ""
      echo "If you want to run without GPU acceleration, run without the --gpu flag."
      exit 1
    fi
  fi

  SERVER_VIDEO_CODEC="h264_nvenc"
  NVCC_PATH=$(command -v nvcc || true)
  if [ -n "$NVCC_PATH" ]; then
    CUDA_DIR=$(dirname $(dirname "$NVCC_PATH"))
    GPU_ARGS="--gpus all -v $CUDA_DIR:$CUDA_DIR -v $NVCC_PATH:$NVCC_PATH"
  else
    echo "Warning: nvcc not found, but GPU was requested."
    GPU_ARGS="--gpus all"
  fi
fi

# Detect number of CPUs for maximum throughput
NUM_CPUS=$(nproc)
CPU_LIST="0-$((NUM_CPUS - 1))"


echo "▶ Starting container: ${CONTAINER_NAME}"
echo "  Image : ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Port  : ${HOST_PORT} → ${CONTAINER_PORT}"
echo "  CPUs  : ${NUM_CPUS} (cores ${CPU_LIST})"
if [ "$USE_GPU" = "true" ]; then
  echo "  GPU   : Enabled (Codec: ${SERVER_VIDEO_CODEC})"
fi

INTERACTIVE_ARGS=""
if [ -t 0 ]; then
  INTERACTIVE_ARGS="--interactive --tty"
fi

# Try to automatically determine the host's Tailscale or external IP for WebRTC
# so it works through Docker NAT without requiring the user to set it.
DETECTED_IP=""
if command -v tailscale >/dev/null 2>&1; then
  DETECTED_IP=$(tailscale ip -4 2>/dev/null || true)
fi
if [ -z "$DETECTED_IP" ]; then
  # Fallback: get the primary non-loopback IPv4 address
  DETECTED_IP=$(ip -4 route get 8.8.8.8 2>/dev/null | awk '{print $7}' || true)
fi

export WEBRTC_PUBLIC_IP="${WEBRTC_PUBLIC_IP:-$DETECTED_IP}"
echo "  WebRTC IP : ${WEBRTC_PUBLIC_IP} (auto-detected)"

docker run \
  $GPU_ARGS \
  --rm \
  $INTERACTIVE_ARGS \
  --name "${CONTAINER_NAME}" \
  --publish "${HOST_PORT}:${CONTAINER_PORT}/tcp" \
  --publish "${HOST_PORT}:${CONTAINER_PORT}/udp" \
  --shm-size 256m \
  --cpuset-cpus "${CPU_LIST}" \
  --ulimit rtprio=99 \
  --cap-add=SYS_NICE \
  --env PORT="${SERVER_PORT}" \
  --env FPS="${SERVER_FPS}" \
  --env DISPLAY_NUM="${SERVER_DISPLAY_NUM}" \
  --env VIDEO_CODEC="${SERVER_VIDEO_CODEC}" \
  --env TEST_PATTERN="${TEST_PATTERN:-}" \
  --env WEBRTC_PUBLIC_IP="${WEBRTC_PUBLIC_IP}" \
  --env HOST_UID=$(id -u) \
  "${IMAGE_NAME}:${IMAGE_TAG}"
