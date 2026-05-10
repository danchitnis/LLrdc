#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_PORT=8080
CONTAINER_NAME="llrdc-hevc-444-test"
CLIENT_BIN="${ROOT_DIR}/dist/llrdc-client-linux-amd64/bin/llrdc-client.bin"

cleanup() {
  echo "▶ Skipping cleanup for inspection..."
  # docker stop "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  # docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "▶ Building container..."
"${ROOT_DIR}/docker-build.sh" --intel

echo "▶ Starting server..."
docker run -d --name "${CONTAINER_NAME}" \
  --network host \
  --device /dev/dri:/dev/dri \
  -e USE_INTEL=true \
  -e INTEL_RENDER_NODE=/dev/dri/renderD130 \
  -e VIDEO_CODEC=h265_qsv \
  -e CHROMA=444 \
  -e CAPTURE_MODE=direct \
  danchitnis/llrdc:intel \
  /app/llrdc --port "${SERVER_PORT}" > /tmp/hevc-444-server.log 2>&1

echo "▶ Waiting for server (up to 30s)..."
for _ in {1..30}; do
  if curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz" >/dev/null 2>&1; then 
    echo "✅ Server ready."
    break 
  fi
  sleep 1
done

echo "▶ Generating client config..."
mkdir -p "${ROOT_DIR}/dist/llrdc-client-linux-amd64/bin/"
cat > "${ROOT_DIR}/dist/llrdc-client-linux-amd64/bin/config.yaml" <<EOF
videoCodec: "hevc_vaapi"
chroma: "444"
EOF

echo "▶ Launching client..."
export SDL_VIDEODRIVER=wayland
export LD_LIBRARY_PATH="${ROOT_DIR}/dist/llrdc-client-linux-amd64/lib"
"${CLIENT_BIN}" --auto-start --stats --server "http://127.0.0.1:${SERVER_PORT}" > /tmp/hevc-444-client.log 2>&1 &
CLIENT_PID=$!

# Monitor client
for _ in {1..20}; do
  if grep -qE "Decoded|First frame|native frame presented" /tmp/hevc-444-client.log; then
    echo "✅ Stream successfully decoded."
    if docker logs "${CONTAINER_NAME}" 2>&1 | grep -qE "profile=rext"; then
      echo "✅ Rext profile confirmed in encoder arguments (high-quality 4:4:4)."
    else
      echo "❌ Rext profile NOT found in encoder arguments."
      docker logs "${CONTAINER_NAME}" 2>&1 | grep -iE "codec|profile" | head -n 20
      exit 1
    fi
    exit 0
  fi
  if ! ps -p $CLIENT_PID > /dev/null; then
    echo "❌ Client crashed."
    tail -n 20 /tmp/hevc-444-client.log
    exit 1
  fi
  sleep 1
done

echo "❌ Timeout waiting for stream."
exit 1
