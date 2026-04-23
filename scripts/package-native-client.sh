#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"
PACKAGE_ROOT="${PACKAGE_ROOT:-dist}"
IMAGE_PLATFORM="${CLIENT_IMAGE_PLATFORM:-linux/amd64}"
# Libraries we expect to be provided by the host's native environment (X11, Wayland, SDL, core C runtime, and Desktop Environment libs like GLib/Pango/Cairo)
SKIP_LIBS_REGEX='^(ld-linux-x86-64\.so\.2|libc\.so\.6|libm\.so\.6|libpthread\.so\.0|libdl\.so\.2|librt\.so\.1|libresolv\.so\.2|libgcc_s\.so\.1|libstdc\+\+\.so\.6|libX.*|libwayland.*|libxcb.*|libxkbcommon.*|libdbus-1\.so\.3|libexpat\.so\.1|libffi\.so\.8|libGL.*|libvulkan.*|libSDL2.*|libglib.*|libgobject.*|libgio.*|libgmodule.*|libpango.*|libcairo.*|libharfbuzz.*|libfontconfig.*|libfreetype.*|libdecor.*|libasound.*|libpulse.*)$'

BUILD_ID="${CLIENT_BUILD_ID:-$(

  {
    find cmd internal -type f -print0
    printf '%s\0' Dockerfile.client go.mod go.sum scripts/package-native-client.sh
  } | xargs -0 sha256sum | sort | sha256sum | cut -c1-16
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
  if [[ "${soname}" =~ ${SKIP_LIBS_REGEX} ]]; then
    printf '%s -> host runtime\n' "${original_path}" >>"${MANIFEST_FILE}"
    continue
  fi
  docker cp "${CONTAINER_ID}:${resolved_path}" "${PACKAGE_DIR}/lib/${soname}"
  printf '%s -> lib/%s (from %s)\n' "${original_path}" "${soname}" "${resolved_path}" >>"${MANIFEST_FILE}"
done <"${LIB_LIST_FILE}"

cat >"${PACKAGE_DIR}/bin/llrdc-client" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
LIB_DIR="${ROOT_DIR}/lib"
BIN_PATH="${SCRIPT_DIR}/llrdc-client.bin"

if [[ -z "${SDL_VIDEODRIVER:-}" ]]; then
  if [[ "${XDG_SESSION_TYPE:-}" == "wayland" && -n "${WAYLAND_DISPLAY:-}" ]]; then
    export SDL_VIDEODRIVER=wayland
  elif [[ "${XDG_SESSION_TYPE:-}" == "wayland" && -n "${DISPLAY:-}" ]]; then
    export SDL_VIDEODRIVER=x11
  elif [[ -n "${DISPLAY:-}" ]]; then
    export SDL_VIDEODRIVER=x11
  fi
fi

if [[ -n "${WAYLAND_DISPLAY:-}" && -n "${XDG_RUNTIME_DIR:-}" && -S "${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}" ]]; then
  :
elif [[ "${SDL_VIDEODRIVER:-}" == "wayland" ]]; then
  echo "Wayland requested but the display socket was not found at ${XDG_RUNTIME_DIR:-<unset>}/${WAYLAND_DISPLAY:-<unset>}" >&2
  exit 1
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
  - Native Wayland is preferred automatically when a Wayland socket is available.
  - X11/Xwayland can still be forced with SDL_VIDEODRIVER=x11.
  - X11 is used automatically only when Wayland is unavailable and DISPLAY is set.

Important:
  - This is a native SDL/libvpx client. It does not embed Chromium, WebView, or WebKit.
  - The package bundles codec runtime libraries and uses the host's native SDL/X11/Wayland/audio stack.
  - Audio and clipboard integration still depend on the host session environment.
EOF

{
  echo "Package: ${PACKAGE_NAME}"
  echo "Image: ${IMAGE_NAME}"
  echo "Platform: ${IMAGE_PLATFORM}"
  echo "BuildID: ${BUILD_ID}"
  echo
  echo "Included runtime libraries:"
  cat "${MANIFEST_FILE}"
} >"${PACKAGE_DIR}/manifest.txt"

tar -C "${PACKAGE_ROOT}" -czf "${PACKAGE_ARCHIVE}" "${PACKAGE_NAME}"

echo "Packaged native host client at ${PACKAGE_DIR}"
echo "Created archive ${PACKAGE_ARCHIVE}"
