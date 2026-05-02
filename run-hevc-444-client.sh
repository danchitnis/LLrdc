#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_WRAPPER="${ROOT_DIR}/dist/llrdc-client-linux-amd64/bin/llrdc-client"
CONFIG_DIR="${ROOT_DIR}/dist/llrdc-client-linux-amd64/bin"
LIB_PATH="${ROOT_DIR}/dist/llrdc-client-linux-amd64/lib"

echo "▶ Generating config.yaml for HEVC 4:4:4..."
mkdir -p "${CONFIG_DIR}"
cat > "${CONFIG_DIR}/config.yaml" <<EOF
videoCodec: "hevc_vaapi"
chroma: "444"
EOF

echo "▶ Launching client..."
export SDL_VIDEODRIVER=wayland
export LD_LIBRARY_PATH="${LIB_PATH}"

"${CLIENT_WRAPPER}" --auto-start --stats --server "http://127.0.0.1:8080"
