#!/usr/bin/env bash
set -euo pipefail

# Run the authoritative native Wayland latency benchmark locally on nzxt5.
# The checkout is expected to already be synchronized; this script never SSHes.

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RUNNER="${ROOT_DIR}/scripts/run-remote-wayland-latency.sh"
ACCELERATOR="${LLRDC_ACCELERATOR:-nvidia}"
MODE=""
SAMPLES="20"
WARMUP="20"
DESTINATION="labwc"
SKIP_BUILD="0"
KEEP_CONTAINERS="0"
ARTEFACT_DIR=""

usage() {
  cat <<'EOF'
Usage: scripts/benchmark-latency.sh [options]

Run the native Wayland latency benchmark on this host.

Accelerators:
  cpu       H.264 software encode; compat mode only
  intel     H.264 VAAPI direct path (compat available with --mode compat)
  nvidia    H.264 NVENC direct path (compat available with --mode compat)

Options:
  --accel NAME         cpu, intel, or nvidia (default: nvidia)
  --mode MODE          compat or direct (default: direct for GPU, compat for CPU)
  --warmup N           Warm-up samples per run (default: 20)
  --samples N          Measured samples per run (default: 20; warm-up is separate)
  --destination MODE   labwc or gnome (default: labwc)
  --artefacts DIR      Output directory (default: timestamped .artefact/<accelerator>)
  --skip-build         Reuse the existing Docker image
  --keep-containers    Keep temporary benchmark containers for inspection
  -h, --help           Show this help

Examples:
  scripts/benchmark-latency.sh --accel cpu
  scripts/benchmark-latency.sh --accel intel
  scripts/benchmark-latency.sh --accel nvidia --mode direct --samples 20
EOF
}

error() {
  echo "❌ $*" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --accel|-a)
      [[ $# -ge 2 ]] || error "--accel requires cpu, intel, or nvidia"
      ACCELERATOR="$2"
      shift 2
      ;;
    --mode)
      [[ $# -ge 2 ]] || error "--mode requires compat or direct"
      MODE="$2"
      shift 2
      ;;
    --warmup)
      [[ $# -ge 2 ]] || error "--warmup requires a non-negative integer"
      WARMUP="$2"
      shift 2
      ;;
    --samples)
      [[ $# -ge 2 ]] || error "--samples requires a positive integer"
      SAMPLES="$2"
      shift 2
      ;;
    --destination)
      [[ $# -ge 2 ]] || error "--destination requires labwc or gnome"
      DESTINATION="$2"
      shift 2
      ;;
    --artefacts)
      [[ $# -ge 2 ]] || error "--artefacts requires a directory"
      ARTEFACT_DIR="$2"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD="1"
      shift
      ;;
    --keep-containers)
      KEEP_CONTAINERS="1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      error "unknown option: $1 (use --help for usage)"
      ;;
  esac
done

case "${ACCELERATOR}" in
  cpu)
    CODEC="h264"
    DEFAULT_MODE="compat"
    ;;
  intel)
    CODEC="h264_vaapi"
    DEFAULT_MODE="direct"
    ;;
  nvidia)
    CODEC="h264_nvenc"
    DEFAULT_MODE="direct"
    ;;
  *)
    error "--accel must be cpu, intel, or nvidia"
    ;;
esac

if [[ -z "${MODE}" ]]; then
  MODE="${DEFAULT_MODE}"
fi
case "${MODE}" in
  compat|direct) ;;
  *) error "invalid mode '${MODE}'" ;;
esac
if [[ "${ACCELERATOR}" == "cpu" && "${MODE}" != "compat" ]]; then
  error "the CPU lane supports compat mode only"
fi
case "${DESTINATION}" in
  labwc|gnome) ;;
  *) error "invalid destination '${DESTINATION}'" ;;
esac
[[ "${WARMUP}" =~ ^[0-9]+$ ]] || error "warmup must be a non-negative integer"
[[ "${SAMPLES}" =~ ^[1-9][0-9]*$ ]] || error "samples must be a positive integer"

[[ -x "${RUNNER}" ]] || error "missing benchmark runner: ${RUNNER}"
command -v docker >/dev/null 2>&1 || error "Docker is required"
command -v python3 >/dev/null 2>&1 || error "Python 3 is required"

if [[ -z "${ARTEFACT_DIR}" ]]; then
  ARTEFACT_DIR="${ROOT_DIR}/.artefact/${ACCELERATOR}/$(date +%Y%m%d-%H%M%S)"
elif [[ "${ARTEFACT_DIR}" != /* ]]; then
  ARTEFACT_DIR="${ROOT_DIR}/${ARTEFACT_DIR}"
fi
mkdir -p "${ARTEFACT_DIR}"

env_args=(
  "LLRDC_ACCELERATOR=${ACCELERATOR}"
  "LLRDC_VIDEO_CODEC=${CODEC}"
  "LLRDC_WARMUP_COUNT=${WARMUP}"
  "LLRDC_SAMPLE_COUNT=${SAMPLES}"
  "LLRDC_DESTINATION_COMPOSITOR=${DESTINATION}"
  "LLRDC_ARTEFACT_DIR=${ARTEFACT_DIR}"
  "LLRDC_KEEP_CONTAINERS=${KEEP_CONTAINERS}"
)

if [[ "${SKIP_BUILD}" == "1" ]]; then
  env_args+=("LLRDC_SKIP_BUILD=1")
fi

env_args+=("LLRDC_COMPARE_MODES=0" "LLRDC_CAPTURE_MODE=${MODE}")

echo "▶ Native Wayland latency benchmark"
echo "  Accelerator : ${ACCELERATOR}"
echo "  Codec       : ${CODEC}"
echo "  Mode        : ${MODE}"
echo "  Destination : ${DESTINATION}"
echo "  Warm-up     : ${WARMUP} samples"
echo "  Measured    : ${SAMPLES} samples per run"
echo "  Artefacts   : ${ARTEFACT_DIR}"
echo

if env "${env_args[@]}" "${RUNNER}"; then
  echo
  echo "✅ Benchmark completed"
  echo "  Report : ${ARTEFACT_DIR}/latency-report.json"
else
  status=$?
  echo "❌ Benchmark failed; inspect ${ARTEFACT_DIR}" >&2
  exit "${status}"
fi
