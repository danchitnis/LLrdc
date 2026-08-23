#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/playwright-browser-env.sh"

usage() {
    cat <<'EOF'
Usage: ./test-nvidia-browser.sh --capture-mode compat|direct [options]

The NVIDIA server must already be running on nzxt5.
Options:
  --capture-mode MODE  Required: compat or direct
  --server-host HOST   Server host (default: nzxt5)
  --port PORT          Server HTTP port (default: 8080)
  --help               Show this help
EOF
}

capture_mode="${LLRDC_CAPTURE_MODE:-}"
server_host="${LLRDC_SERVER_HOST:-}"
server_port="${LLRDC_SERVER_PORT:-8080}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --capture-mode)
            capture_mode="${2:-}"
            shift 2
            ;;
        --server-host)
            server_host="${2:-}"
            shift 2
            ;;
        --port)
            server_port="${2:-}"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if [ "$capture_mode" != "compat" ] && [ "$capture_mode" != "direct" ]; then
    echo "--capture-mode must be compat or direct." >&2
    usage >&2
    exit 2
fi
if ! [[ "$server_port" =~ ^[0-9]+$ ]] || [ "$server_port" -lt 1 ] || [ "$server_port" -gt 65535 ]; then
    echo "--port must be a valid TCP port." >&2
    exit 2
fi

case "$(uname -s)" in
    Darwin)
        server_host="${server_host:-nzxt5}"
        expected_transport="WebTransport"
        ;;
    Linux)
        server_host="${server_host:-nzxt5}"
        expected_transport="WebTransport"
        ;;
    *)
        echo "Unsupported Playwright host platform: $(uname -s)" >&2
        exit 1
        ;;
esac

require_playwright_runtime
configure_installed_chrome
export LLRDC_ACCELERATOR=nvidia
export LLRDC_CAPTURE_MODE="$capture_mode"
export LLRDC_SERVER_HOST="$server_host"
export LLRDC_SERVER_PORT="$server_port"
export LLRDC_EXPECTED_TRANSPORT="$expected_transport"

exec npx playwright test tests/nvidia/gpu_connection.spec.ts --workers=1 --reporter=line --timeout=60000
