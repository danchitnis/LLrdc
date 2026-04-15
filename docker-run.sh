#!/usr/bin/env bash
# docker-run.sh — Run the llrdc Docker container.
set -euo pipefail

IMAGE_TAG_EXPLICIT="false"

if [[ -v IMAGE_TAG ]]; then
  IMAGE_TAG_EXPLICIT="true"
fi

IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="${CONTAINER_NAME:-llrdc}"

SERVER_PORT="${PORT:-8080}"
SERVER_FPS="${FPS:-30}"
SERVER_BANDWIDTH="${BANDWIDTH:-5}"
SERVER_VBR="${VBR:-false}"
SERVER_DAMAGE_TRACKING="${DAMAGE_TRACKING:-false}"
SERVER_VIDEO_CODEC="${VIDEO_CODEC:-h264}"
SERVER_CAPTURE_MODE="${CAPTURE_MODE:-compat}"
SERVER_RESOLUTION="${RESOLUTION:-0}"

# Port mappings (override via env vars)
HOST_PORT="${HOST_PORT:-8080}"
CONTAINER_PORT="${CONTAINER_PORT:-$SERVER_PORT}"
USE_NVIDIA="false"
USE_INTEL="false"
USE_DETACHED="false"
USE_HOST_NET="false"
USE_DEBUG_FFMPEG="false"
USE_DEBUG_INPUT="false"
WEBRTC_INTERFACES_ENV=""
WEBRTC_INTERFACES="${WEBRTC_INTERFACES:-}"
WEBRTC_EXCLUDE_INTERFACES="${WEBRTC_EXCLUDE_INTERFACES:-}"
SERVER_HDPI="${HDPI:-0}"
HOST_RENDER_GID="${RENDER_GID:-}"
HOST_VIDEO_GID="${VIDEO_GID:-}"

WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-}"
WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-}"
ACTIVITY_PULSE_HZ="${ACTIVITY_PULSE_HZ:-}"
ACTIVITY_TIMEOUT="${ACTIVITY_TIMEOUT:-}"
NVENC_LATENCY_MODE="${NVENC_LATENCY_MODE:-}"

detect_image_variant() {
  local image_ref="$1"
  local variant
  variant=$(docker image inspect --format '{{ index .Config.Labels "com.danchitnis.llrdc.build-variant" }}' "$image_ref" 2>/dev/null || true)
  if [ "$variant" = "<no value>" ]; then
    variant=""
  fi
  printf '%s' "$variant"
}

ensure_image_exists() {
  local image_ref="$1"
  local intel_requested="$2"
  if docker image inspect "$image_ref" >/dev/null 2>&1; then
    return 0
  fi

  echo "❌ ERROR: Docker image ${image_ref} is not available locally."
  if [ "$intel_requested" = "true" ]; then
    echo "Build it with: ./docker-build.sh --intel"
  else
    echo "Build it with: ./docker-build.sh"
  fi
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--detach)
      USE_DETACHED="true"
      shift
      ;;
    --host-net)
      USE_HOST_NET="true"
      shift
      ;;
    --intel)
      USE_INTEL="true"
      shift
      ;;
    --webrtc-buffer)
      if [ -n "${2:-}" ]; then
        WEBRTC_BUFFER_SIZE="$2"
        shift 2
      else
        echo "Error: --webrtc-buffer requires an argument."
        exit 1
      fi
      ;;
    --webrtc-low-latency)
      WEBRTC_LOW_LATENCY="true"
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
    --iface|-i)
      if [ -n "${2:-}" ]; then
        WEBRTC_INTERFACES="$2"
        WEBRTC_INTERFACES_ENV="$2"
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
      shift
      ;;
  esac
done

if [ "$USE_INTEL" = "true" ] && [ "$IMAGE_TAG_EXPLICIT" = "false" ]; then
  IMAGE_TAG="intel"
fi

IMAGE_REF="${IMAGE_NAME}:${IMAGE_TAG}"
ensure_image_exists "${IMAGE_REF}" "${USE_INTEL}"
IMAGE_VARIANT="$(detect_image_variant "${IMAGE_REF}")"

if [ "$USE_INTEL" = "true" ]; then
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
  if [ -z "${VIDEO_CODEC:-}" ]; then
    SERVER_VIDEO_CODEC="h264_qsv"
  fi
  if [ -d /dev/dri ]; then
    GPU_ARGS="--device /dev/dri:/dev/dri"
    for node in /dev/dri/card* /dev/dri/renderD*; do
      if [ -e "$node" ]; then
        GPU_ARGS="$GPU_ARGS --device $node:$node"
      fi
    done
    if [ -z "$HOST_RENDER_GID" ] && [ -e /dev/dri/renderD128 ]; then
      HOST_RENDER_GID=$(stat -c '%g' /dev/dri/renderD128)
    fi
    if [ -z "$HOST_VIDEO_GID" ] && [ -e /dev/dri/card0 ]; then
      HOST_VIDEO_GID=$(stat -c '%g' /dev/dri/card0)
    fi
  else
    echo "Warning: /dev/dri not found, but Intel GPU was requested."
  fi
fi

if [ "$USE_NVIDIA" = "true" ]; then
  # Verify if Docker has NVIDIA runtime/toolkit support
  if ! docker info 2>/dev/null | grep -qi "Runtimes.*nvidia"; then
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
fi

# Detect number of CPUs for maximum throughput
NUM_CPUS=$(nproc)
CPU_LIST="0-$((NUM_CPUS - 1))"


echo "▶ Starting container: ${CONTAINER_NAME}"
echo "  Image : ${IMAGE_REF}"

NETWORK_ARGS=""
if [ "$USE_HOST_NET" = "true" ]; then
  NETWORK_ARGS="--network host"
  echo "  Net   : Host (--network host)"
else
  NETWORK_ARGS="--publish ${HOST_PORT}:${CONTAINER_PORT}/tcp --publish ${HOST_PORT}:${CONTAINER_PORT}/udp"
  echo "  Port  : ${HOST_PORT} → ${CONTAINER_PORT} (TCP/UDP)"
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

# Detect IP for WebRTC only when explicitly provided or when an interface was pinned.
# Otherwise the server derives the advertised IP from the incoming request host.
if [ -z "${WEBRTC_PUBLIC_IP:-}" ] && [ -n "${WEBRTC_INTERFACES:-}" ]; then
  WEBRTC_PUBLIC_IP=$(ip -4 addr show "${WEBRTC_INTERFACES%%,*}" | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n1 || true)
fi

if [ -n "${WEBRTC_PUBLIC_IP:-}" ]; then
  echo "  WebRTC IP : ${WEBRTC_PUBLIC_IP}"
else
  echo "  WebRTC IP : request-host derived"
fi
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
  $NETWORK_ARGS \
  $UINPUT_ARGS \
  --rm \
  $INTERACTIVE_ARGS \
  --name "${CONTAINER_NAME}" \
  --shm-size 256m \
  --cpuset-cpus "${CPU_LIST}" \
  --ulimit rtprio=99 \
  --cap-add=SYS_NICE \
  --cap-add=SYS_ADMIN \
  --env PORT="${SERVER_PORT}" \
  --env FPS="${SERVER_FPS}" \
  --env BANDWIDTH="${SERVER_BANDWIDTH}" \
  --env VBR="${SERVER_VBR}" \
  --env DAMAGE_TRACKING="${SERVER_DAMAGE_TRACKING}" \
  --env VIDEO_CODEC="${SERVER_VIDEO_CODEC}" \
  --env USE_NVIDIA="${USE_NVIDIA}" \
  --env USE_INTEL="${USE_INTEL}" \
  --env LIBVA_DRIVER_NAME="iHD" \
  --env CAPTURE_MODE="${SERVER_CAPTURE_MODE}" \
  --env TEST_PATTERN="${TEST_PATTERN:-}" \
  --env WEBRTC_PUBLIC_IP="${WEBRTC_PUBLIC_IP:-}" \
  --env WEBRTC_INTERFACES="${WEBRTC_INTERFACES_ENV}" \
  --env WEBRTC_EXCLUDE_INTERFACES="${WEBRTC_EXCLUDE_INTERFACES:-}" \
  --env WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-}" \
  --env WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-}" \
  --env ACTIVITY_PULSE_HZ="${ACTIVITY_PULSE_HZ:-}" \
  --env ACTIVITY_TIMEOUT="${ACTIVITY_TIMEOUT:-}" \
  --env CPU_EFFORT="${CPU_EFFORT:-}" \
  --env NVENC_LATENCY_MODE="${NVENC_LATENCY_MODE:-}" \
  --env ENABLE_AUDIO="${ENABLE_AUDIO:-false}" \
  --env AUDIO_BITRATE="${AUDIO_BITRATE:-128k}" \
  --env HDPI="${SERVER_HDPI}" \
  --env RESOLUTION="${SERVER_RESOLUTION}" \
  --env USE_DEBUG_FFMPEG="${USE_DEBUG_FFMPEG}" \
  --env USE_DEBUG_INPUT="${USE_DEBUG_INPUT}" \
  --env RENDER_GID="${HOST_RENDER_GID}" \
  --env VIDEO_GID="${HOST_VIDEO_GID}" \
  --env HOST_UID=$(id -u) \
  "${IMAGE_REF}"
