#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="${CLIENT_IMAGE_NAME:-llrdc-client:native}"
PACKAGE_ROOT="${PACKAGE_ROOT:-dist}"
IMAGE_PLATFORM="${CLIENT_IMAGE_PLATFORM:-linux/amd64}"
SKIP_LIBS_REGEX='^(ld-linux-x86-64\.so\.2|libc\.so\.6|libm\.so\.6|libpthread\.so\.0|libdl\.so\.2|librt\.so\.1|libresolv\.so\.2)$'
INCLUDE_BUNDLED_LIBS_REGEX='^(libvpx\.so(\..*)?)$'
BUILD_ID="${CLIENT_BUILD_ID:-$(
  {
    find cmd internal -type f -print0
    printf '%s\0' Dockerfile.client go.mod go.sum
  } | xargs -0 sha256sum | sort | sha256sum | cut -c1-16
)}"

DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" docker build \
  --platform "${IMAGE_PLATFORM}" \
  --build-arg "CLIENT_BUILD_ID=${BUILD_ID}" \
  -f Dockerfile.client \
  -t "${IMAGE_NAME}" .

ARCH="$(docker image inspect "${IMAGE_NAME}" --format '{{.Architecture}}')"
OS="$(docker image inspect "${IMAGE_NAME}" --format '{{.Os}}')"
PACKAGE_NAME="llrdc-client-${OS}-${ARCH}"
PACKAGE_DIR="${PACKAGE_ROOT}/${PACKAGE_NAME}"
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
rm -f "${PACKAGE_ROOT}/${PACKAGE_NAME}.tar.gz"
printf '%s\n' "${BUILD_ID}" >"${PACKAGE_DIR}/BUILD_ID"

docker cp "${CONTAINER_ID}:/usr/local/bin/llrdc-client" "${PACKAGE_DIR}/bin/llrdc-client.bin"

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
  if [[ ! "${soname}" =~ ${INCLUDE_BUNDLED_LIBS_REGEX} ]]; then
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

chmod +x "${PACKAGE_DIR}/bin/llrdc-client" "${PACKAGE_DIR}/bin/llrdc-client.bin"

cat >"${PACKAGE_DIR}/README.txt" <<EOF
LLrdc Native Client
===================

This package was built inside Docker and is intended to run directly on the Linux host.

Run:
  ./bin/llrdc-client --server http://127.0.0.1:8080 --control-addr 127.0.0.1:18080

Display backend selection:
  - X11/Xwayland is preferred automatically on Wayland sessions when DISPLAY is available.
  - Native Wayland can still be forced with SDL_VIDEODRIVER=wayland.
  - X11 is used automatically when DISPLAY is set and Wayland/Xwayland is available.

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

tar -C "${PACKAGE_ROOT}" -czf "${PACKAGE_ROOT}/${PACKAGE_NAME}.tar.gz" "${PACKAGE_NAME}"

echo "Packaged native host client at ${PACKAGE_DIR}"
echo "Created archive ${PACKAGE_ROOT}/${PACKAGE_NAME}.tar.gz"
