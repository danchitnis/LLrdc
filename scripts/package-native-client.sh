#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"
PACKAGE_ROOT="${PACKAGE_ROOT:-dist}"
IMAGE_PLATFORM="${CLIENT_IMAGE_PLATFORM:-linux/amd64}"
FFMPEG_RELEASE_BASE="${FFMPEG_RELEASE_BASE:-https://ffmpeg.org/releases}"
FFMPEG_VERSION="${FFMPEG_VERSION:-auto}"
GLIBC_MAX_VERSION="${GLIBC_MAX_VERSION:-2.39}"
BUNDLED_LIBS_REGEX='^lib(avcodec|avutil|avswresample|swscale)\.so(\..*)?$'
FORBIDDEN_BUNDLED_LIBS_REGEX='^(libvpx|libX|libxcb|libwayland|libSDL2|libasound|libpulse|libGL|libEGL|libdrm|libgbm|libsystemd|libapparmor|libcap)(\.|$)'

resolve_ffmpeg_version() {
  if [[ "${FFMPEG_VERSION}" != "auto" ]]; then
    printf '%s\n' "${FFMPEG_VERSION}"
    return 0
  fi
  local releases
  releases="$(curl -fsSL "${FFMPEG_RELEASE_BASE}/" | grep -oE 'ffmpeg-[0-9]+(\.[0-9]+)*\.tar\.xz' || true)"
  printf '%s\n' "${releases}" \
    | sed -E 's/^ffmpeg-//; s/\.tar\.xz$//' \
    | sort -Vu \
    | tail -1
}

RESOLVED_FFMPEG_VERSION="$(resolve_ffmpeg_version)"
if [[ -z "${RESOLVED_FFMPEG_VERSION}" ]]; then
  echo "Failed to resolve FFmpeg release version" >&2
  exit 1
fi
FFMPEG_SOURCE_URL="${FFMPEG_RELEASE_BASE}/ffmpeg-${RESOLVED_FFMPEG_VERSION}.tar.xz"
FFMPEG_SOURCE_SHA256="${FFMPEG_SOURCE_SHA256:-$(curl -fsSL "${FFMPEG_SOURCE_URL}" | sha256sum | awk '{print $1}')}"

BUILD_ID="${CLIENT_BUILD_ID:-$(

  {
    {
      find cmd internal client -type f -print0
      printf '%s\0' Dockerfile.client go.mod go.sum scripts/package-native-client.sh
    } | xargs -0 sha256sum
    printf '%s  %s\n' "${RESOLVED_FFMPEG_VERSION}" "FFMPEG_VERSION"
    printf '%s  %s\n' "${FFMPEG_SOURCE_SHA256}" "FFMPEG_SOURCE_SHA256"
  } | sort | sha256sum | cut -c1-16
)}"

FORCE_REBUILD=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force-rebuild|--rebuild)
      FORCE_REBUILD=1
      shift
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

PLATFORM_OS="${IMAGE_PLATFORM%%/*}"
PLATFORM_ARCH="${IMAGE_PLATFORM##*/}"
PACKAGE_NAME="llrdc-client-${PLATFORM_OS}-${PLATFORM_ARCH}"
PACKAGE_DIR="${PACKAGE_ROOT}/${PACKAGE_NAME}"
PACKAGE_ARCHIVE="${PACKAGE_ROOT}/${PACKAGE_NAME}.tar.gz"

if [[ "${FORCE_REBUILD}" -eq 0 ]] \
  && [[ -f "${PACKAGE_DIR}/BUILD_ID" ]] \
  && [[ -x "${PACKAGE_DIR}/bin/llrdc-client" ]] \
  && [[ -x "${PACKAGE_DIR}/bin/llrdc-client.bin" ]] \
  && [[ -x "${PACKAGE_DIR}/bin/linux-uinput-bench" ]] \
  && [[ -x "${PACKAGE_DIR}/bin/linux-uinput-bench.bin" ]] \
  && [[ -f "${PACKAGE_ARCHIVE}" ]] \
  && [[ "$(tr -d '[:space:]' < "${PACKAGE_DIR}/BUILD_ID")" == "${BUILD_ID}" ]]; then
  echo "Reusing native client package at ${PACKAGE_DIR} (BUILD_ID ${BUILD_ID})"
  exit 0
fi

DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" docker build \
  --platform "${IMAGE_PLATFORM}" \
  --build-arg "CLIENT_BUILD_ID=${BUILD_ID}" \
  --build-arg "FFMPEG_VERSION=${RESOLVED_FFMPEG_VERSION}" \
  --build-arg "FFMPEG_SOURCE_SHA256=${FFMPEG_SOURCE_SHA256}" \
  -f Dockerfile.client \
  -t "${IMAGE_NAME}" .

ARCH="$(docker image inspect "${IMAGE_NAME}" --format '{{.Architecture}}')"
OS="$(docker image inspect "${IMAGE_NAME}" --format '{{.Os}}')"
PACKAGE_NAME="llrdc-client-${OS}-${ARCH}"
PACKAGE_DIR="${PACKAGE_ROOT}/${PACKAGE_NAME}"
PACKAGE_ARCHIVE="${PACKAGE_ROOT}/${PACKAGE_NAME}.tar.gz"
CONTAINER_ID="$(docker create "${IMAGE_NAME}")"
LIB_LIST_FILE="$(mktemp)"
MANIFEST_FILE="$(mktemp)"

cleanup() {
  docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true
  rm -f "${LIB_LIST_FILE}" "${MANIFEST_FILE}"
}
trap cleanup EXIT

rm -rf "${PACKAGE_DIR}"
mkdir -p "${PACKAGE_DIR}/bin" "${PACKAGE_DIR}/lib"
rm -f "${PACKAGE_ARCHIVE}"
printf '%s\n' "${BUILD_ID}" >"${PACKAGE_DIR}/BUILD_ID"

docker cp "${CONTAINER_ID}:/usr/local/bin/llrdc-client" "${PACKAGE_DIR}/bin/llrdc-client.bin"
docker cp "${CONTAINER_ID}:/usr/local/bin/linux-uinput-bench" "${PACKAGE_DIR}/bin/linux-uinput-bench.bin"

docker run --rm --platform "${IMAGE_PLATFORM}" --entrypoint /bin/sh "${IMAGE_NAME}" -lc \
  'ldd /usr/local/bin/llrdc-client \
    | sed -nE "s/.*=> (\/[^ ]+).*/\1/p; s/^[[:space:]]*(\/[^ ]+).*/\1/p" \
    | sort -u \
    | while read -r path; do
        resolved="$(readlink -f "${path}")"
        printf "%s\t%s\n" "${path}" "${resolved}"
      done' >"${LIB_LIST_FILE}"

while IFS=$'\t' read -r original_path resolved_path; do
  [[ -n "${original_path}" ]] || continue
  [[ -n "${resolved_path}" ]] || continue
  soname="$(basename "${original_path}")"
  if [[ ! "${soname}" =~ ${BUNDLED_LIBS_REGEX} ]]; then
    continue
  fi
  docker cp "${CONTAINER_ID}:${resolved_path}" "${PACKAGE_DIR}/lib/${soname}"
  printf '%s -> lib/%s (from %s)\n' "${original_path}" "${soname}" "${resolved_path}" >>"${MANIFEST_FILE}"
done <"${LIB_LIST_FILE}"

if find "${PACKAGE_DIR}/lib" -maxdepth 1 -type f -printf '%f\n' | grep -Eq "${FORBIDDEN_BUNDLED_LIBS_REGEX}"; then
  echo "Package contains forbidden host/runtime libraries:" >&2
  find "${PACKAGE_DIR}/lib" -maxdepth 1 -type f -printf '%f\n' | grep -E "${FORBIDDEN_BUNDLED_LIBS_REGEX}" >&2
  exit 1
fi

if readelf -d "${PACKAGE_DIR}/bin/llrdc-client.bin" | grep -q 'Shared library: \[libvpx'; then
  echo "Native client unexpectedly links libvpx; VP8 should decode through FFmpeg/libavcodec" >&2
  exit 1
fi

max_glibc_required() {
  { readelf --version-info "$@" 2>/dev/null | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)*' || true; } \
    | sed 's/^GLIBC_//' \
    | sort -Vu \
    | tail -1
}

while IFS= read -r artifact; do
  required="$(max_glibc_required "${artifact}")"
  [[ -n "${required}" ]] || continue
  highest="$(printf '%s\n%s\n' "${required}" "${GLIBC_MAX_VERSION}" | sort -Vu | tail -1)"
  if [[ "${highest}" != "${GLIBC_MAX_VERSION}" ]]; then
    echo "${artifact} requires GLIBC_${required}, above supported GLIBC_${GLIBC_MAX_VERSION}" >&2
    exit 1
  fi
done < <(find "${PACKAGE_DIR}/bin" "${PACKAGE_DIR}/lib" -type f \( -name '*.bin' -o -name '*.so*' \) -print)

cat >"${PACKAGE_DIR}/bin/llrdc-client" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
LIB_DIR="${ROOT_DIR}/lib"
BIN_PATH="${SCRIPT_DIR}/llrdc-client.bin"

headless=0
for arg in "$@"; do
  if [[ "${arg}" == "--headless" ]]; then
    headless=1
    break
  fi
done

if [[ "${headless}" -eq 0 ]]; then
  export SDL_VIDEODRIVER=wayland
  export WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}"
  if [[ -z "${XDG_RUNTIME_DIR:-}" || ! -S "${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}" ]]; then
    echo "Wayland display socket was not found at ${XDG_RUNTIME_DIR:-<unset>}/${WAYLAND_DISPLAY}" >&2
    exit 1
  fi
fi

if [[ ! -x "${BIN_PATH}" ]]; then
  echo "Missing client binary at ${BIN_PATH}" >&2
  exit 1
fi

if compgen -G "${LIB_DIR}/*" >/dev/null 2>&1; then
  export LD_LIBRARY_PATH="${LIB_DIR}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
fi
exec "${BIN_PATH}" "$@"
EOF

cat >"${PACKAGE_DIR}/bin/linux-uinput-bench" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
LIB_DIR="${ROOT_DIR}/lib"
BIN_PATH="${SCRIPT_DIR}/linux-uinput-bench.bin"

if [[ ! -x "${BIN_PATH}" ]]; then
  echo "Missing bench injector binary at ${BIN_PATH}" >&2
  exit 1
fi

if compgen -G "${LIB_DIR}/*" >/dev/null 2>&1; then
  export LD_LIBRARY_PATH="${LIB_DIR}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
fi
exec "${BIN_PATH}" "$@"
EOF

chmod +x \
  "${PACKAGE_DIR}/bin/llrdc-client" \
  "${PACKAGE_DIR}/bin/llrdc-client.bin" \
  "${PACKAGE_DIR}/bin/linux-uinput-bench" \
  "${PACKAGE_DIR}/bin/linux-uinput-bench.bin"

cat >"${PACKAGE_DIR}/README.txt" <<EOF
LLrdc Native Client
===================

This package was built inside Docker and is intended to run directly on the Linux host.

Run:
  ./bin/llrdc-client --server http://127.0.0.1:8080 --control-addr 127.0.0.1:18080

Latency bench injector:
  ./bin/linux-uinput-bench

Display backend selection:
  - Native Linux client runtime is Wayland-only.
  - The host must provide XDG_RUNTIME_DIR and a WAYLAND_DISPLAY socket.
  - WAYLAND_DISPLAY defaults to wayland-0 when unset.

Important:
  - This is a native SDL client. It does not embed Chromium, WebView, or WebKit.
  - VP8 and H.264 are decoded through the bundled FFmpeg codec libraries.
  - The package uses the host's native SDL/Wayland/audio stack.
  - Audio and clipboard integration still depend on the host session environment.
EOF

{
  echo "Package: ${PACKAGE_NAME}"
  echo "Image: ${IMAGE_NAME}"
  echo "Platform: ${IMAGE_PLATFORM}"
  echo "BuildID: ${BUILD_ID}"
  echo "FFmpegVersion: ${RESOLVED_FFMPEG_VERSION}"
  echo "FFmpegSource: ${FFMPEG_SOURCE_URL}"
  echo "FFmpegSourceSHA256: ${FFMPEG_SOURCE_SHA256}"
  echo "MaxSupportedGLIBC: ${GLIBC_MAX_VERSION}"
  echo
  echo "Bundled runtime libraries:"
  cat "${MANIFEST_FILE}"
} >"${PACKAGE_DIR}/manifest.txt"

tar -C "${PACKAGE_ROOT}" -czf "${PACKAGE_ARCHIVE}" "${PACKAGE_NAME}"

echo "Packaged native host client at ${PACKAGE_DIR}"
echo "Created archive ${PACKAGE_ARCHIVE}"
