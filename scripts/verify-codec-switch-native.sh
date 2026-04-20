#!/usr/bin/env bash
set -euo pipefail

pick_port() {
    local start="$1"
    local end="$2"
    local port
    for ((port = start; port <= end; port++)); do
        if ! ss -ltnH "( sport = :${port} )" 2>/dev/null | grep -q .; then
            echo "${port}"
            return 0
        fi
    done
    echo "Unable to find a free TCP port in range ${start}-${end}" >&2
    exit 1
}

SERVER_PORT="${SERVER_PORT:-$(pick_port 18090 18190)}"
CONTROL_PORT="${CONTROL_PORT:-$(pick_port 18191 18291)}"
SERVER_NAME="llrdc-verify-server-${SERVER_PORT}"
CLIENT_BIN="./dist/llrdc-client-linux-amd64/bin/llrdc-client"

FORCE_REBUILD=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force-rebuild|--rebuild) FORCE_REBUILD=1; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done

echo "1. Building server image and packaging Linux native client..."
./docker-build.sh > /dev/null
if [ "$FORCE_REBUILD" -eq 1 ]; then
    ./scripts/package-native-client.sh --force-rebuild > /dev/null
else
    ./scripts/package-native-client.sh > /dev/null
fi

if [[ ! -x "${CLIENT_BIN}" ]]; then
    echo "Missing packaged client at ${CLIENT_BIN}" >&2
    exit 1
fi

echo "2. Starting local server (Linux native CPU codec test: VP8 -> H264)..."
docker rm -f "${SERVER_NAME}" >/dev/null 2>&1 || true
PORT=${SERVER_PORT} ./docker-run.sh --detach --name "${SERVER_NAME}" --host-net --test-pattern --video-codec vp8 > /dev/null

cleanup() {
    echo "Cleaning up..."
    [ -n "${CLIENT_PID:-}" ] && kill "${CLIENT_PID}" >/dev/null 2>&1 || true
    docker rm -f "${SERVER_NAME}" > /dev/null 2>&1 || true
}
trap cleanup EXIT

echo "3. Launching native client (Headed directly on host)..."
"${CLIENT_BIN}" \
    --server "http://127.0.0.1:${SERVER_PORT}" \
    --control-addr "127.0.0.1:${CONTROL_PORT}" \
    --auto-start > /tmp/llrdc-verify-client.log 2>&1 &
CLIENT_PID=$!

echo "4. Waiting for client to be ready and presenting VP8 frames..."
for i in {1..20}; do
    STATS=$(curl -s --max-time 1 "http://127.0.0.1:${CONTROL_PORT}/statsz" || echo "")
    if [ -n "$STATS" ]; then
        presented=$(echo "$STATS" | grep -oP '"presentedFrames":\s*\K\d+' || echo "0")
        if [ "$presented" -gt 5 ]; then
            echo "   Currently presenting VP8 frames: $presented"
            break
        fi
    fi
    [ "$i" -eq 20 ] && { echo "Timeout waiting for VP8 frames"; exit 1; }
    sleep 1
done

echo "5. Switching codec to H.264 via Control API..."
curl -s -X POST -H "Content-Type: application/json" \
    -d '{"id":"codec.set:h264"}' \
    "http://127.0.0.1:${CONTROL_PORT}/command" > /dev/null

echo "6. Verifying stream recovery (H264)..."
LAST_FRAMES=-1
PROGRESS_SEEN=0
SUCCESS=0
for i in {1..10}; do
    echo -n "   [$i/10] "
    STATS=$(curl -s --max-time 1 "http://127.0.0.1:${CONTROL_PORT}/statsz" || echo "")
    if [ -z "$STATS" ]; then
        echo "Client API unresponsive (reconnecting)..."
    else
        PRESENTED=$(echo "$STATS" | grep -oP '"presentedFrames":\s*\K\d+' || echo "0")
        if [ "$LAST_FRAMES" -ge 0 ] && [ "$PRESENTED" -lt "$LAST_FRAMES" ]; then
            LAST_FRAMES=$PRESENTED
            echo "Stats reset observed, continuing after reconnect (presentedFrames=$PRESENTED)"
            sleep 1
            continue
        fi
        if [ "$LAST_FRAMES" -ge 0 ] && [ "$PRESENTED" -gt "$LAST_FRAMES" ]; then
            PROGRESS_SEEN=$((PROGRESS_SEEN + 1))
            echo "H264 Progress: presentedFrames=$PRESENTED (progress samples=$PROGRESS_SEEN)"
            if [ "$PROGRESS_SEEN" -ge 3 ] && [ "$PRESENTED" -ge 10 ]; then
                SUCCESS=1
                break
            fi
        else
            echo "Still at $PRESENTED frames..."
        fi
        LAST_FRAMES=$PRESENTED
    fi
    sleep 1
done

if [ "$SUCCESS" -eq 1 ]; then
    echo "SUCCESS: Native codec switch verified. Stream recovered and is incrementing frames."
else
    echo "FAILURE: Stream frozen after codec switch."
    echo "--- Client Logs ---"
    cat /tmp/llrdc-verify-client.log
    echo "--- Server Logs ---"
    docker logs "${SERVER_NAME}"
    exit 1
fi
