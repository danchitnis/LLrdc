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

USE_GPU="false"
USE_WAYLAND="false"
USE_DETACHED="false"
USE_DEBUG_X11="false"
USE_DEBUG_FFMPEG="false"
USE_DEBUG_INPUT="false"
WEBRTC_INTERFACES="${WEBRTC_INTERFACES:-}"
WEBRTC_EXCLUDE_INTERFACES="${WEBRTC_EXCLUDE_INTERFACES:-}"
SERVER_HDPI="${HDPI:-0}"
CONTAINER_WEBRTC_INTERFACES=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--detach)
      USE_DETACHED="true"
      shift
      ;;
    --wayland)
      USE_WAYLAND="true"
      IMAGE_TAG="wayland-latest"
      shift
      ;;
    --gpu)
      USE_GPU="true"
      shift
      ;;
    --debug-x11)
      USE_DEBUG_X11="true"
      shift
      ;;
    --debug-ffmpeg)
      USE_DEBUG_FFMPEG="true"
      shift
      ;;
    --debug-input)
      USE_DEBUG_INPUT="true"
      shift
      ;;
    --debug)
      USE_DEBUG_X11="true"
      USE_DEBUG_FFMPEG="true"
      USE_DEBUG_INPUT="true"
      shift
      ;;
    --iface|-i)
      if [ -n "${2:-}" ]; then
        WEBRTC_INTERFACES="$2"
        shift 2
      else
        echo "Error: --iface requires an argument."
        exit 1
      fi
      ;;
    --exclude-iface|-x)
      if [ -n "${2:-}" ]; then
        WEBRTC_EXCLUDE_INTERFACES="$2"
        shift 2
      else
        echo "Error: --exclude-iface requires an argument."
        exit 1
      fi
      ;;
    --name)
      if [ -n "${2:-}" ]; then
        CONTAINER_NAME="$2"
        shift 2
      else
        echo "Error: --name requires an argument."
        exit 1
      fi
      ;;
    --hdpi|-h)
      if [[ -n "${2:-}" ]] && [[ "$2" =~ ^[0-9]+$ ]]; then
        SERVER_HDPI="$2"
        shift 2
      else
        SERVER_HDPI="200"
        shift
      fi
      ;;
    *)
      shift
      ;;
  esac
done

if [ "$USE_WAYLAND" = "true" ]; then
  if [ "$USE_GPU" = "false" ]; then
    SERVER_VIDEO_CODEC="vp8"
    echo "  Mode  : Wayland (Minimal VP8 CPU)"
  else
    echo "  Mode  : Wayland (GPU)"
  fi
fi

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

  if [ -z "${VIDEO_CODEC:-}" ]; then
    SERVER_VIDEO_CODEC="h264_nvenc"
  fi
  NVCC_PATH=$(command -v nvcc || true)
  if [ -n "$NVCC_PATH" ]; then
    CUDA_DIR=$(dirname $(dirname "$NVCC_PATH"))
    GPU_ARGS="--gpus all -v $CUDA_DIR:$CUDA_DIR -v $NVCC_PATH:$NVCC_PATH -e NVIDIA_DRIVER_CAPABILITIES=all"
  else
    echo "Warning: nvcc not found, but GPU was requested."
    GPU_ARGS="--gpus all -e NVIDIA_DRIVER_CAPABILITIES=all"
  fi
fi

# Detect number of CPUs for maximum throughput
NUM_CPUS=$(nproc)
CPU_LIST="0-$((NUM_CPUS - 1))"


echo "▶ Starting container: ${CONTAINER_NAME}"
echo "  Image : ${IMAGE_NAME}:${IMAGE_TAG}"
echo "  Port  : ${HOST_PORT} → ${CONTAINER_PORT}"
echo "  CPUs  : ${NUM_CPUS} (cores ${CPU_LIST})"
if [ "${USE_DEBUG:-false}" = "true" ]; then
  echo "  FPS   : ${SERVER_FPS}"
fi
if [ "$USE_GPU" = "true" ]; then
  echo "  GPU   : Enabled (Codec: ${SERVER_VIDEO_CODEC})"
fi

INTERACTIVE_ARGS=""
if [ -t 0 ] && [ "$USE_DETACHED" = "false" ]; then
  INTERACTIVE_ARGS="--interactive --tty"
fi

DETACHED_ARGS=""
if [ "$USE_DETACHED" = "true" ]; then
  DETACHED_ARGS="--detach"
fi

# Detect IP for WebRTC. 
# WEBRTC_PUBLIC_IP is what Pion will put in the ICE candidates.
# If the user provides one, use it.
if [ -z "${WEBRTC_PUBLIC_IP:-}" ]; then
  # If we have an explicit interface preference, use that IP
  if [ -n "${WEBRTC_INTERFACES:-}" ]; then
    WEBRTC_PUBLIC_IP=$(ip -4 addr show "${WEBRTC_INTERFACES%%,*}" | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n1 || true)
  fi

  # Fallback to general detection
  if [ -z "${WEBRTC_PUBLIC_IP:-}" ]; then
    WEBRTC_PUBLIC_IP=$(ip -4 route get 8.8.8.8 2>/dev/null | awk '{print $7}' || true)
  fi
fi

echo "  WebRTC IP : ${WEBRTC_PUBLIC_IP:-none} (auto-detected)"
if [ -n "${WEBRTC_INTERFACES:-}" ]; then
  echo "  WebRTC Iface : ${WEBRTC_INTERFACES} (host IP selection only in Docker bridge mode)"
fi
UINPUT_ARGS=""
if [ -e /dev/uinput ]; then
  UINPUT_ARGS="--device /dev/uinput:/dev/uinput"
fi

docker run \
  $GPU_ARGS \
  $DETACHED_ARGS \
  $UINPUT_ARGS \
  --rm \
  $INTERACTIVE_ARGS \
  --name "${CONTAINER_NAME}" \
  --publish "${HOST_PORT}:${CONTAINER_PORT}/tcp" \
  --publish "${HOST_PORT}:${CONTAINER_PORT}/udp" \
  --shm-size 256m \
  --cpuset-cpus "${CPU_LIST}" \
  --ulimit rtprio=99 \
  --cap-add=SYS_NICE \
  --cap-add=SYS_ADMIN \
  --env PORT="${SERVER_PORT}" \
  --env FPS="${SERVER_FPS}" \
  --env DISPLAY_NUM="${SERVER_DISPLAY_NUM}" \
  --env VIDEO_CODEC="${SERVER_VIDEO_CODEC}" \
  --env USE_GPU="${USE_GPU}" \
  --env TEST_PATTERN="${TEST_PATTERN:-}" \
  --env WEBRTC_PUBLIC_IP="${WEBRTC_PUBLIC_IP}" \
  --env WEBRTC_INTERFACES="${CONTAINER_WEBRTC_INTERFACES}" \
  --env WEBRTC_EXCLUDE_INTERFACES="${WEBRTC_EXCLUDE_INTERFACES:-}" \
  --env HDPI="${SERVER_HDPI}" \
  --env USE_DEBUG_X11="${USE_DEBUG_X11}" \
  --env USE_DEBUG_FFMPEG="${USE_DEBUG_FFMPEG}" \
  --env USE_DEBUG_INPUT="${USE_DEBUG_INPUT}" \
  --env HOST_UID=$(id -u) \
  "${IMAGE_NAME}:${IMAGE_TAG}"
