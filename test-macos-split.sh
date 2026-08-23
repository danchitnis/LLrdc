#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "macOS split browser tests require Darwin; no Linux or nzxt5 execution is supported." >&2
    exit 2
fi

CHROME_APP="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
SAFARI_DRIVER="/usr/bin/safaridriver"
IMAGE="danchitnis/llrdc:macos"
BASE_PORT="${PORT:-8080}"
ARTIFACT_ROOT="${MACOS_TEST_ARTIFACT_ROOT:-.artefact/macos-browser}"
BROWSER_FILTER=""
SCENARIO_FILTER=""
SCENARIOS=(connection reconfiguration resolution hdpi input clipboard codecs)
SAFARI_SCENARIOS=(connection reconfiguration resolution hdpi input codecs)
BROWSERS=(chrome safari)

usage() {
    cat <<'EOF'
Usage: ./test-macos-split.sh [--browser chrome|safari] [--scenario NAME]

Runs the local native macOS server and Docker capture agent in each scenario,
then drives the installed headed Chrome and Safari sequentially.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --browser) BROWSER_FILTER="${2:-}"; shift 2 ;;
        --scenario) SCENARIO_FILTER="${2:-}"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

contains() {
    local needle="$1"
    shift
    local value
    for value in "$@"; do
        [[ "$value" == "$needle" ]] && return 0
    done
    return 1
}

if [[ -n "$BROWSER_FILTER" ]] && ! contains "$BROWSER_FILTER" "${BROWSERS[@]}"; then
    echo "Unsupported browser: $BROWSER_FILTER" >&2
    exit 2
fi
if [[ -n "$SCENARIO_FILTER" ]] && ! contains "$SCENARIO_FILTER" "${SCENARIOS[@]}"; then
    echo "Unsupported scenario: $SCENARIO_FILTER" >&2
    exit 2
fi
if [[ "$BROWSER_FILTER" == "safari" ]] && [[ "$SCENARIO_FILTER" == "clipboard" ]]; then
    echo "Safari does not implement clipboard synchronization; clipboard is not a Safari scenario." >&2
    exit 2
fi

for command in docker curl npm go npx lsof; do
    command -v "$command" >/dev/null || { echo "Required command not found: $command" >&2; exit 2; }
done
[[ -x "$CHROME_APP" ]] || { echo "Installed Google Chrome not found at $CHROME_APP" >&2; exit 2; }
[[ -x "$SAFARI_DRIVER" ]] || { echo "Safari WebDriver not found at $SAFARI_DRIVER" >&2; exit 2; }
docker info >/dev/null 2>&1 || { echo "Docker Desktop is not running" >&2; exit 2; }

for port in "$BASE_PORT" "$((BASE_PORT + 10))" 12345 12346 12348; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
        echo "Required local port is already in use: $port" >&2
        exit 2
    fi
done

mkdir -p "$ARTIFACT_ROOT"
echo "Building frontend, native macOS server, and local Docker capture image..."
npm run build
go build -o macos-server ./server/macos/*.go
./docker-build.sh --macos

run_combo() {
    local browser="$1"
    local scenario="$2"
    local combo_dir="${ARTIFACT_ROOT}/${browser}-${scenario}"
    local container="llrdc-macos-${browser}-${scenario}"
    local server_log="${combo_dir}/macos-server.log"
    local status=0
    local server_pid=""

    mkdir -p "$combo_dir"
    rm -f "$server_log" "${combo_dir}/container.log"
    docker rm -f "$container" >/dev/null 2>&1 || true

    cleanup_combo() {
        if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
            kill "$server_pid" 2>/dev/null || true
            wait "$server_pid" 2>/dev/null || true
        fi
        docker logs "$container" >"${combo_dir}/container.log" 2>&1 || true
        docker rm -f "$container" >/dev/null 2>&1 || true
    }
    trap cleanup_combo RETURN

    echo "=== ${browser}/${scenario}: starting fresh local server and capture agent ==="
    PORT="$BASE_PORT" USE_DEBUG_INPUT=true ./macos-server >"$server_log" 2>&1 &
    server_pid=$!
    for attempt in {1..30}; do
        if curl -fsS "http://127.0.0.1:${BASE_PORT}/healthz" >/dev/null 2>&1 && lsof -nP -iTCP:12345 -sTCP:LISTEN >/dev/null 2>&1; then break; fi
        sleep 1
        if [[ "$attempt" == 30 ]]; then echo "macos-server did not become ready; see $server_log" >&2; return 1; fi
    done

    docker run -d \
        --name "$container" \
        --shm-size=2gb \
        -e USE_DEBUG_INPUT=true \
        -p 12346:12346 \
        -p 12348:12348 \
        --add-host host.docker.internal:host-gateway \
        "$IMAGE" >/dev/null

    for attempt in {1..30}; do
        if grep -q "Video producer connected" "$server_log"; then break; fi
        sleep 1
        if [[ "$attempt" == 30 ]]; then echo "Docker capture agent did not connect; see $server_log" >&2; return 1; fi
    done

    set +e
    MACOS_TEST_BASE_URL="http://127.0.0.1:${BASE_PORT}" \
    MACOS_TEST_CONTAINER="$container" \
    MACOS_TEST_ARTIFACT_DIR="$combo_dir" \
        npx tsx tests/macos-safari/run.ts --browser "$browser" --scenario "$scenario"
    status=$?
    set -e
    if [[ "$status" -ne 0 ]]; then echo "FAIL ${browser}/${scenario}; artifacts: $combo_dir" >&2; else echo "PASS ${browser}/${scenario}"; fi
    return "$status"
}

selected_browsers=("${BROWSERS[@]}")
selected_scenarios=("${SCENARIOS[@]}")
[[ -n "$BROWSER_FILTER" ]] && selected_browsers=("$BROWSER_FILTER")
[[ -n "$SCENARIO_FILTER" ]] && selected_scenarios=("$SCENARIO_FILTER")
if [[ "$SCENARIO_FILTER" == "clipboard" ]] && [[ -z "$BROWSER_FILTER" ]]; then
    echo "Skipping Safari clipboard: Safari clipboard synchronization is not implemented."
    selected_browsers=(chrome)
fi

for browser in "${selected_browsers[@]}"; do
    if [[ "$browser" == "safari" ]] && [[ -z "$SCENARIO_FILTER" ]]; then
        selected_scenarios=("${SAFARI_SCENARIOS[@]}")
    fi
    for scenario in "${selected_scenarios[@]}"; do
        run_combo "$browser" "$scenario"
    done
done

echo "All selected local macOS installed-browser scenarios passed."
