#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This probe is intended to run on macOS." >&2
  exit 1
fi

for cmd in docker curl jq go lsof; do
  if [[ "${cmd}" == "docker" ]] && ! command -v "${cmd}" >/dev/null 2>&1; then
    continue
  fi
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Missing required command: ${cmd}" >&2
    exit 1
  fi
done

get_free_port() {
  local port
  port=$((RANDOM % 1000 + 18080))
  while lsof -Pi :"${port}" -sTCP:LISTEN -t >/dev/null 2>&1; do
    port=$((RANDOM % 1000 + 18080))
  done
  echo "${port}"
}

SERVER_PORT="${SERVER_PORT:-$(get_free_port)}"
CONTROL_PORT="${CONTROL_PORT:-$(get_free_port)}"
WINDOW_WIDTH="${WINDOW_WIDTH:-1280}"
WINDOW_HEIGHT="${WINDOW_HEIGHT:-720}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-30}"
CONTAINER_NAME="${CONTAINER_NAME:-llrdc-macos-dimension-probe-${SERVER_PORT}}"
CLIENT_BIN="${CLIENT_BIN:-/tmp/llrdc-macos-client-probe}"
CLIENT_LOG="${CLIENT_LOG:-/tmp/llrdc-macos-client-probe.log}"
SERVER_BIN="${SERVER_BIN:-/tmp/llrdc-server-probe}"
SERVER_LOG="${SERVER_LOG:-/tmp/llrdc-server-probe.log}"
USE_DOCKER="false"

if command -v docker >/dev/null 2>&1; then
  USE_DOCKER="true"
fi

cleanup() {
  if [[ -n "${CLIENT_PID:-}" ]]; then
    kill "${CLIENT_PID}" >/dev/null 2>&1 || true
    wait "${CLIENT_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
  if [[ "${USE_DOCKER}" == "true" ]]; then
    docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ "${USE_DOCKER}" == "true" ]]; then
  echo "▶ Building local Docker server image"
  (cd "${ROOT_DIR}" && ./docker-build.sh)
else
  echo "▶ Docker not found, using local --test-pattern server"
  if ! command -v ffmpeg >/dev/null 2>&1; then
    echo "Missing required command for local fallback: ffmpeg" >&2
    exit 1
  fi
fi

echo "▶ Building server"
(cd "${ROOT_DIR}" && go build -o "${SERVER_BIN}" ./cmd/server)
echo "▶ Building macOS native client"
(cd "${ROOT_DIR}" && go build -tags native -o "${CLIENT_BIN}" ./cmd/client/main.go)

if [[ "${USE_DOCKER}" == "true" ]]; then
  echo "▶ Starting Docker server on port ${SERVER_PORT}"
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  (
    cd "${ROOT_DIR}" && \
    PORT="${SERVER_PORT}" HOST_PORT="${SERVER_PORT}" VIDEO_CODEC=h264 VBR=false ./docker-run.sh --detach --name "${CONTAINER_NAME}" --debug-input --res 1080p >/dev/null
  )
else
  echo "▶ Starting local test-pattern server on port ${SERVER_PORT}"
  rm -f "${SERVER_LOG}"
  "${SERVER_BIN}" --port "${SERVER_PORT}" --test-pattern --video-codec h264 --fps 30 --enable-audio=false >"${SERVER_LOG}" 2>&1 &
  SERVER_PID=$!
fi

echo "▶ Waiting for server readiness"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while true; do
  if server_ready=$(curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz" 2>/dev/null); then
    if [[ "$(echo "${server_ready}" | jq -r '.ready')" == "true" ]]; then
      break
    fi
  fi
  if (( SECONDS >= deadline )); then
    echo "Server did not become ready in time" >&2
    if [[ "${USE_DOCKER}" == "true" ]]; then
      docker logs "${CONTAINER_NAME}" || true
    else
      cat "${SERVER_LOG}" || true
    fi
    exit 1
  fi
  sleep 1
done

echo "▶ Launching macOS native client"
rm -f "${CLIENT_LOG}"
"${CLIENT_BIN}" \
  --server "http://127.0.0.1:${SERVER_PORT}" \
  --control-addr "127.0.0.1:${CONTROL_PORT}" \
  --width "${WINDOW_WIDTH}" \
  --height "${WINDOW_HEIGHT}" \
  --exit-after "${TIMEOUT_SECONDS}s" \
  >"${CLIENT_LOG}" 2>&1 &
CLIENT_PID=$!

echo "▶ Waiting for client to connect and present"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while true; do
  if client_state=$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/statez" 2>/dev/null); then
    webrtc_connected="$(echo "${client_state}" | jq -r '.webrtcConnected')"
    presenting="$(echo "${client_state}" | jq -r '.presenting')"
    last_presented_width="$(echo "${client_state}" | jq -r '.lastPresentedWidth // 0')"
    last_resize_width="$(echo "${client_state}" | jq -r '.lastResizeWidth // 0')"
    if [[ "${webrtc_connected}" == "true" && "${presenting}" == "true" && "${last_presented_width}" != "0" && "${last_resize_width}" != "0" ]]; then
      break
    fi
  fi
  if (( SECONDS >= deadline )); then
    echo "Client did not become ready in time" >&2
    cat "${CLIENT_LOG}" || true
    exit 1
  fi
  sleep 1
done

server_ready="$(curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz")"
client_ready="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/readyz")"
client_state="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/statez")"

window_width="$(echo "${client_state}" | jq -r '.windowWidth // 0')"
window_height="$(echo "${client_state}" | jq -r '.windowHeight // 0')"
resize_width="$(echo "${client_state}" | jq -r '.lastResizeWidth // 0')"
resize_height="$(echo "${client_state}" | jq -r '.lastResizeHeight // 0')"
presented_width="$(echo "${client_state}" | jq -r '.lastPresentedWidth // 0')"
presented_height="$(echo "${client_state}" | jq -r '.lastPresentedHeight // 0')"
client_server_width="$(echo "${client_state}" | jq -r '.serverScreenWidth // 0')"
client_server_height="$(echo "${client_state}" | jq -r '.serverScreenHeight // 0')"
server_width="$(echo "${server_ready}" | jq -r '.screenWidth // 0')"
server_height="$(echo "${server_ready}" | jq -r '.screenHeight // 0')"

echo "▶ Dimension chain"
echo "  window content : ${window_width}x${window_height}"
echo "  resize sent    : ${resize_width}x${resize_height}"
echo "  client server  : ${client_server_width}x${client_server_height}"
echo "  server readyz  : ${server_width}x${server_height}"
echo "  presented      : ${presented_width}x${presented_height}"

failed=0

if [[ "${window_width}" != "${resize_width}" || "${window_height}" != "${resize_height}" ]]; then
  echo "❌ Window content size does not match the resize sent." >&2
  failed=1
fi

if [[ "${server_width}" != "${resize_width}" || "${server_height}" != "${resize_height}" ]]; then
  echo "❌ Server screen size does not match the resize sent." >&2
  failed=1
fi

if [[ "${client_server_width}" != "${server_width}" || "${client_server_height}" != "${server_height}" ]]; then
  echo "❌ Client-observed server size does not match server readyz." >&2
  failed=1
fi

if [[ "${presented_width}" != "${server_width}" || "${presented_height}" != "${server_height}" ]]; then
  echo "❌ Presented frame size does not match server screen size." >&2
  failed=1
fi

if [[ "${failed}" -ne 0 ]]; then
  echo "▶ Client log"
  cat "${CLIENT_LOG}" || true
  echo "▶ Server log"
  if [[ "${USE_DOCKER}" == "true" ]]; then
    docker logs "${CONTAINER_NAME}" || true
  else
    cat "${SERVER_LOG}" || true
  fi
  exit 1
fi

echo "✅ Dimension chain matches end to end."
