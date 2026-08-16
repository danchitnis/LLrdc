#!/usr/bin/env bash
# docker-run.sh — Run the llrdc Docker container.
set -euo pipefail

IMAGE_TAG_EXPLICIT="false"

if [ -n "${IMAGE_TAG+x}" ]; then
  IMAGE_TAG_EXPLICIT="true"
fi

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="${CONTAINER_NAME:-llrdc}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/scripts/docker/common.sh"

SERVER_PORT="${PORT:-8080}"
SERVER_FPS="${FPS:-30}"
SERVER_BANDWIDTH="${BANDWIDTH:-5}"
SERVER_VBR="${VBR:-false}"
SERVER_DAMAGE_TRACKING="${DAMAGE_TRACKING:-false}"
SERVER_VIDEO_CODEC="${VIDEO_CODEC:-h264}"
SERVER_CHROMA="${CHROMA:-420}"
SERVER_CAPTURE_MODE="${CAPTURE_MODE:-compat}"
SERVER_RESOLUTION="${RESOLUTION:-0}"

# Port mappings (override via env vars)
HOST_PORT="${HOST_PORT:-8080}"
CONTAINER_PORT="${CONTAINER_PORT:-$SERVER_PORT}"
USE_NVIDIA="false"
USE_INTEL="false"
USE_DETACHED="false"
USE_HOST_NET="false"
USE_DRY_RUN="false"
USE_DEBUG_FFMPEG="${USE_DEBUG_FFMPEG:-false}"
USE_DEBUG_INPUT="${USE_DEBUG_INPUT:-false}"
SERVER_HDPI="${HDPI:-0}"
HOST_RENDER_GID="${RENDER_GID:-}"
HOST_VIDEO_GID="${VIDEO_GID:-}"
INTEL_RENDER_NODE="${INTEL_RENDER_NODE:-/dev/dri/renderD128}"

ACTIVITY_PULSE_HZ="${ACTIVITY_PULSE_HZ:-}"
ACTIVITY_TIMEOUT="${ACTIVITY_TIMEOUT:-}"
NVENC_LATENCY_MODE="${NVENC_LATENCY_MODE:-}"
CLIENT_TIMEOUT="${CLIENT_TIMEOUT:-10}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --*=*)
      KEY="${1%%=*}"
      VALUE="${1#*=}"
      shift
      set -- "$KEY" "$VALUE" "$@"
      continue
      ;;
    --dry-run)
      USE_DRY_RUN="true"
      shift
      ;;
    -d|--detach)
      USE_DETACHED="true"
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
    --host-net|--network-host)
      USE_HOST_NET="true"
      shift
      ;;
    --network)
      if [ "${2:-}" = "host" ]; then
        USE_HOST_NET="true"
        shift 2
      else
        echo "Error: Only --network host is currently supported as a pass-through alias."
        exit 1
      fi
      ;;
    --intel)
      USE_INTEL="true"
      shift
      ;;
    --intel-device)
      if [ -n "${2:-}" ]; then
        INTEL_RENDER_NODE="/dev/dri/render$2"
        USE_INTEL="true"
        shift 2
      else
        echo "Error: --intel-device requires an argument (e.g., D130)."
        exit 1
      fi
      ;;
    --chroma-444)
      SERVER_CHROMA="444"
      CHROMA="444"
      shift
      ;;
    --activity-hz)
      if [ -n "${2:-}" ]; then
        ACTIVITY_PULSE_HZ="$2"
        shift 2
      else
        echo "Error: --activity-hz requires an argument."
        exit 1
      fi
      ;;
    --activity-timeout)
      if [ -n "${2:-}" ]; then
        ACTIVITY_TIMEOUT="$2"
        shift 2
      else
        echo "Error: --activity-timeout requires an argument."
        exit 1
      fi
      ;;
    --no-nvenc-latency)
      NVENC_LATENCY_MODE="false"
      shift
      ;;
    --nvidia)
      USE_NVIDIA="true"
      shift
      ;;
    --video-codec)
      if [ -n "${2:-}" ]; then
        SERVER_VIDEO_CODEC="$2"
        VIDEO_CODEC="$2" # Also set the env-var equivalent to override defaults
        shift 2
      else
        echo "Error: --video-codec requires an argument."
        exit 1
      fi
      ;;
    --capture-mode)
      if [ -n "${2:-}" ]; then
        SERVER_CAPTURE_MODE="$2"
        shift 2
      else
        echo "Error: --capture-mode requires an argument."
        exit 1
      fi
      ;;
    --direct-buffer)
      SERVER_CAPTURE_MODE="direct"
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
      USE_DEBUG_FFMPEG="true"
      USE_DEBUG_INPUT="true"
      shift
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
    --res)
      if [ -n "${2:-}" ]; then
        SERVER_RESOLUTION="$2"
        shift 2
      else
        echo "Error: --res requires an argument (e.g. 720p, 1080p)."
        exit 1
      fi
      ;;
    *)
      echo "Error: Unknown argument: $1"
      exit 1
      ;;
  esac
done

if [ "$IMAGE_TAG_EXPLICIT" = "false" ]; then
  if [ "$USE_INTEL" = "true" ]; then
    IMAGE_TAG="intel"
  elif [ "$USE_NVIDIA" = "true" ]; then
    IMAGE_TAG="nvidia"
  fi
fi

IMAGE_REF="${IMAGE_NAME}:${IMAGE_TAG}"
if [ "$USE_DRY_RUN" = "false" ]; then
  ensure_image_exists "${IMAGE_REF}" "${USE_INTEL}" "${USE_NVIDIA}"
fi
IMAGE_VARIANT="$(detect_image_variant "${IMAGE_REF}")"

if [ "$USE_INTEL" = "true" ] && { [ "$USE_DRY_RUN" = "false" ] || [ -n "$IMAGE_VARIANT" ]; }; then
  case "${IMAGE_VARIANT}" in
    intel)
      ;;
    cpu)
      echo "❌ ERROR: Docker image ${IMAGE_REF} is a CPU-only build."
      echo "Use ./docker-build.sh --intel to build the Intel image, or run without --intel."
      exit 1
      ;;
    "")
      echo "Warning: Docker image ${IMAGE_REF} does not expose an LLrdc build-variant label."
      echo "Assuming it is a legacy Intel-capable image."
      ;;
    *)
      echo "Warning: Docker image ${IMAGE_REF} reports unknown build variant '${IMAGE_VARIANT}'."
      ;;
  esac
fi

if [ "$USE_NVIDIA" = "true" ] && { [ "$USE_DRY_RUN" = "false" ] || [ -n "$IMAGE_VARIANT" ]; }; then
  case "${IMAGE_VARIANT}" in
    nvidia)
      ;;
    cpu)
      echo "❌ ERROR: Docker image ${IMAGE_REF} is a CPU-only build."
      echo "Use ./docker-build.sh --nvidia to build the NVIDIA image, or run without --nvidia."
      exit 1
      ;;
    "")
      echo "Warning: Docker image ${IMAGE_REF} does not expose an LLrdc build-variant label."
      echo "Assuming it is a legacy NVIDIA-capable image."
      ;;
    *)
      echo "Warning: Docker image ${IMAGE_REF} reports unknown build variant '${IMAGE_VARIANT}'."
      ;;
  esac
fi

if [ "$USE_NVIDIA" = "false" ] && [ "$USE_INTEL" = "false" ]; then
  SERVER_VIDEO_CODEC="${VIDEO_CODEC:-vp8}"
  echo "  Mode  : Wayland (Minimal ${SERVER_VIDEO_CODEC} CPU)"
elif [ "$USE_INTEL" = "true" ]; then
  echo "  Mode  : Wayland (Intel GPU)"
else
  echo "  Mode  : Wayland (NVIDIA GPU)"
fi

if [ "$SERVER_CAPTURE_MODE" = "direct" ] && [ "$USE_NVIDIA" != "true" ] && [ "$USE_INTEL" != "true" ]; then
  echo "❌ ERROR: --capture-mode direct requires --nvidia or --intel."
  exit 1
fi

GPU_ARGS=""
if [ "$USE_INTEL" = "true" ]; then
  if [ -z "${SERVER_VIDEO_CODEC:-}" ] || [ "$SERVER_VIDEO_CODEC" = "vp8" ]; then
    SERVER_VIDEO_CODEC="h264_qsv"
  fi
  if [ -d /dev/dri ]; then
    GPU_ARGS="--device /dev/dri:/dev/dri"
    for node in /dev/dri/card* /dev/dri/renderD*; do
      if [ -e "$node" ]; then
        GPU_ARGS="$GPU_ARGS --device $node:$node"
      fi
    done
    if [ -z "$HOST_RENDER_GID" ]; then
      if [ -e "$INTEL_RENDER_NODE" ]; then
        HOST_RENDER_GID=$(stat -c '%g' "$INTEL_RENDER_NODE")
      fi
    fi
    if [ -z "$HOST_VIDEO_GID" ]; then
      if [ -e /dev/dri/by-path/pci-0000:03:00.0-card ]; then
        HOST_VIDEO_GID=$(stat -Lc '%g' /dev/dri/by-path/pci-0000:03:00.0-card)
      elif [ -e /dev/dri/card0 ]; then
        HOST_VIDEO_GID=$(stat -c '%g' /dev/dri/card0)
      fi
    fi
  else
    echo "Warning: /dev/dri not found, but Intel GPU was requested."
  fi
fi

if [ "$USE_NVIDIA" = "true" ]; then
  # Verify if Docker has NVIDIA runtime/toolkit support
  if [ "$USE_DRY_RUN" = "false" ] && ! docker info 2>/dev/null | grep -qi "Runtimes.*nvidia"; then
    if ! docker info 2>/dev/null | grep -qi "nvidia"; then
      echo "❌ ERROR: Docker does not appear to support NVIDIA GPUs."
      echo "Please install the NVIDIA Container Toolkit and restart Docker."
      echo "  https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html"
      echo ""
      echo "If you want to run without NVIDIA acceleration, run without the --nvidia flag."
      exit 1
    fi
  fi

  if [ -z "${VIDEO_CODEC:-}" ]; then
    SERVER_VIDEO_CODEC="h264_nvenc"
  fi
  NVCC_PATH=$(command -v nvcc || true)
  if [ -n "$NVCC_PATH" ]; then
    CUDA_DIR=$(dirname $(dirname "$NVCC_PATH"))
    GPU_ARGS="$GPU_ARGS --gpus all -v $CUDA_DIR:$CUDA_DIR -v $NVCC_PATH:$NVCC_PATH -e NVIDIA_DRIVER_CAPABILITIES=all"
  else
    echo "Warning: nvcc not found, but GPU was requested."
    GPU_ARGS="$GPU_ARGS --gpus all -e NVIDIA_DRIVER_CAPABILITIES=all"
  fi
  if [ "$SERVER_CAPTURE_MODE" = "direct" ] && [ -d /dev/dri ] && ! echo "$GPU_ARGS" | grep -q "/dev/dri"; then
    GPU_ARGS="$GPU_ARGS --device /dev/dri:/dev/dri"
    if [ -z "$HOST_RENDER_GID" ] && [ -e /dev/dri/renderD128 ]; then
      HOST_RENDER_GID=$(stat -c '%g' /dev/dri/renderD128)
    fi
    if [ -z "$HOST_VIDEO_GID" ] && [ -e /dev/dri/card0 ]; then
      HOST_VIDEO_GID=$(stat -c '%g' /dev/dri/card0)
    fi
  fi
  # Enable privileged mode for direct capture to bypass driver-level unprivileged memory export/import blocks
  if [ "$SERVER_CAPTURE_MODE" = "direct" ]; then
    GPU_ARGS="$GPU_ARGS --privileged"
    # Mount host GBM and NVIDIA Allocator libraries into the container
    if [ -d /usr/lib/x86_64-linux-gnu/gbm ]; then
      GPU_ARGS="$GPU_ARGS -v /usr/lib/x86_64-linux-gnu/gbm:/usr/lib/x86_64-linux-gnu/gbm:ro"
    fi
    for lib in /usr/lib/x86_64-linux-gnu/libnvidia-allocator.so*; do
      if [ -e "$lib" ] && [ ! -L "$lib" ]; then
        GPU_ARGS="$GPU_ARGS -v $lib:$lib:ro"
      fi
    done
    # Mount Vulkan ICD configurations to enable headless Vulkan-CUDA interop
    if [ -d /usr/share/vulkan/icd.d ]; then
      GPU_ARGS="$GPU_ARGS -v /usr/share/vulkan/icd.d:/usr/share/vulkan/icd.d:ro"
    fi
  fi
fi

# Detect number of CPUs for maximum throughput
if command -v nproc &> /dev/null; then
  NUM_CPUS=$(nproc)
elif command -v sysctl &> /dev/null; then
  # Fallback for macOS
  NUM_CPUS=$(sysctl -n hw.logicalcpu)
else
  NUM_CPUS=4 # Safe fallback
fi
CPU_LIST="0-$((NUM_CPUS - 1))"


echo "▶ Starting container: ${CONTAINER_NAME}"
echo "  Image : ${IMAGE_REF}"

NETWORK_ARGS=""
if [ "$USE_HOST_NET" = "true" ]; then
  NETWORK_ARGS="--network host"
  echo "  Net   : Host (--network host)"
else
  WT_HOST_PORT=$((HOST_PORT + 10))
  WT_CONTAINER_PORT=$((CONTAINER_PORT + 10))
  # WebTransport port must be published for both TCP (for initial HTTPS handshake) and UDP (for HTTP/3)
  NETWORK_ARGS="--publish ${HOST_PORT}:${CONTAINER_PORT}/tcp --publish ${WT_HOST_PORT}:${WT_CONTAINER_PORT}/tcp --publish ${WT_HOST_PORT}:${WT_CONTAINER_PORT}/udp"
  echo "  Port  : ${HOST_PORT} → ${CONTAINER_PORT} (TCP)"
  echo "  WebTransport: ${WT_HOST_PORT} → ${WT_CONTAINER_PORT} (TCP/UDP)"
fi

echo "  CPUs  : ${NUM_CPUS} (cores ${CPU_LIST})"
if [ "${USE_DEBUG:-false}" = "true" ]; then
  echo "  FPS   : ${SERVER_FPS}"
fi
if [ "$USE_NVIDIA" = "true" ]; then
  echo "  GPU   : Enabled (Codec: ${SERVER_VIDEO_CODEC})"
fi
echo "  Capture Mode : ${SERVER_CAPTURE_MODE}"

INTERACTIVE_ARGS=""
if [ -t 0 ] && [ "$USE_DETACHED" = "false" ]; then
  INTERACTIVE_ARGS="--interactive --tty"
fi

DETACHED_ARGS=""
if [ "$USE_DETACHED" = "true" ]; then
  DETACHED_ARGS="--detach"
fi

UINPUT_ARGS=""
if [ -e /dev/uinput ]; then
  UINPUT_ARGS="--device /dev/uinput:/dev/uinput"
fi

mkdir -p "${SCRIPT_DIR}/certs"

LIBVA_DRIVER_NAME_ENV="iHD"
if [ "${USE_NVIDIA}" = "true" ]; then
  LIBVA_DRIVER_NAME_ENV="nvidia"
fi

DOCKER_RUN_CMD=(docker run)
append_words DOCKER_RUN_CMD "$GPU_ARGS"
append_words DOCKER_RUN_CMD "$DETACHED_ARGS"
append_words DOCKER_RUN_CMD "$NETWORK_ARGS"
append_words DOCKER_RUN_CMD "$UINPUT_ARGS"
DOCKER_RUN_CMD+=(
  --rm
  --name "${CONTAINER_NAME}"
  --ipc=host
  --shm-size 256m
  --cpuset-cpus "${CPU_LIST}"
  --ulimit rtprio=99
  --cap-add=SYS_NICE
  --cap-add=SYS_ADMIN
  --cap-add=IPC_LOCK
  -v "${SCRIPT_DIR}/certs:/app/certs"
  --env "CERTS_DIR=/app/certs"
  --env "PORT=${SERVER_PORT}"
  --env "FPS=${SERVER_FPS}"
  --env "BANDWIDTH=${SERVER_BANDWIDTH}"
  --env "VBR=${SERVER_VBR}"
  --env "DAMAGE_TRACKING=${SERVER_DAMAGE_TRACKING}"
  --env "CHROMA=${SERVER_CHROMA}"
  --env "VIDEO_CODEC=${SERVER_VIDEO_CODEC}"
  --env "USE_NVIDIA=${USE_NVIDIA}"
  --env "USE_INTEL=${USE_INTEL}"
  --env "__GL_YIELD=USLEEP"
  --env "__GL_THREADED_OPTIMIZATIONS=1"
  --env "__GL_SYNC_TO_VBLANK=0"
  --env "INTEL_RENDER_NODE=${INTEL_RENDER_NODE}"
  --env "LIBVA_DRIVER_NAME=${LIBVA_DRIVER_NAME_ENV}"
  --env "CAPTURE_MODE=${SERVER_CAPTURE_MODE}"
  --env "TEST_PATTERN=${TEST_PATTERN:-}"
  --env "ACTIVITY_PULSE_HZ=${ACTIVITY_PULSE_HZ:-}"
  --env "ACTIVITY_TIMEOUT=${ACTIVITY_TIMEOUT:-}"
  --env "CPU_EFFORT=${CPU_EFFORT:-}"
  --env "NVENC_LATENCY_MODE=${NVENC_LATENCY_MODE:-}"
  --env "CLIENT_TIMEOUT=${CLIENT_TIMEOUT:-}"
  --env "LLRDC_PRESENTATION_CLOCK_ID=${LLRDC_PRESENTATION_CLOCK_ID:-}"
  --env "ENABLE_AUDIO=${ENABLE_AUDIO:-false}"
  --env "AUDIO_BITRATE=${AUDIO_BITRATE:-128k}"
  --env "HDPI=${SERVER_HDPI}"
  --env "RESOLUTION=${SERVER_RESOLUTION}"
  --env "USE_DEBUG_FFMPEG=${USE_DEBUG_FFMPEG}"
  --env "USE_DEBUG_INPUT=${USE_DEBUG_INPUT}"
  --env "RENDER_GID=${HOST_RENDER_GID}"
  --env "VIDEO_GID=${HOST_VIDEO_GID}"
  --env "HOST_UID=$(id -u)"
)
append_words DOCKER_RUN_CMD "$INTERACTIVE_ARGS"
DOCKER_RUN_CMD+=("${IMAGE_REF}")

if [ "$USE_DRY_RUN" = "true" ]; then
  echo "Dry run command:"
  print_command "${DOCKER_RUN_CMD[@]}"
  exit 0
fi

"${DOCKER_RUN_CMD[@]}"
