#!/usr/bin/env bash
# scripts/test-macos-native-bitrate.sh
# Automates the verification of Bitrate switching on the macOS native client.

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_PORT=8080
CONTROL_PORT=18080
CLIENT_LOG="/tmp/llrdc-macos-client-bitrate-test.log"
SERVER_LOG="/tmp/llrdc-macos-server-bitrate-test.log"

cleanup() {
    echo "▶ Cleaning up..."
    if [ -n "${CLIENT_PID:-}" ]; then
        kill -9 "$CLIENT_PID" 2>/dev/null || true
    fi
    killall -9 llrdc-client 2>/dev/null || true
    if [ -n "${SERVER_PID:-}" ]; then
        kill -9 "$SERVER_PID" 2>/dev/null || true
    fi
    killall -9 macos-server 2>/dev/null || true
    docker rm -f llrdc-macos 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "▶ Starting macOS split server..."
cd "${ROOT_DIR}"
./run-macos-split.sh > "${SERVER_LOG}" 2>&1 &
SERVER_PID=$!

# Give it a few seconds to start building/running
sleep 5

echo "▶ Waiting for host server on port ${SERVER_PORT}..."
for i in {1..60}; do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "❌ ERROR: Server process died during startup."
        cat "${SERVER_LOG}"
        exit 1
    fi
    
    RESPONSE=$(curl -s "http://127.0.0.1:${SERVER_PORT}/readyz" || echo "curl failed")
    if [ "$RESPONSE" != "curl failed" ] && echo "$RESPONSE" | grep -q '"ready":true'; then
        break
    fi
    [ "$i" -eq 60 ] && { echo "❌ Timeout waiting for server"; exit 1; }
    sleep 1
done
echo "✅ Server ready."

echo "▶ Building macOS Native Client..."
./macos/build.sh > /dev/null

echo "▶ Launching macOS Native Client with --stats..."
./macos/LLrdc.app/Contents/MacOS/llrdc-client \
    --server "http://127.0.0.1:${SERVER_PORT}" \
    --control-addr "127.0.0.1:${CONTROL_PORT}" \
    --stats \
    --auto-start > "${CLIENT_LOG}" 2>&1 &
CLIENT_PID=$!

echo "▶ Waiting for client window and initial stream..."
for i in {1..30}; do
    STATE=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/readyz" || echo "{}")
    WEBRTC_OK=$(echo "$STATE" | jq -r '.webtransportConnected')
    
    if [ "$WEBRTC_OK" == "true" ]; then
        STATS=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" || echo "{}")
        FRAMES=$(echo "$STATS" | jq -r '.presentedFrames // 0')
        if [ "$FRAMES" -gt 5 ]; then
            echo "✅ Window opened and initial stream active ($FRAMES frames)."
            break
        fi
    fi
    [ "$i" -eq 30 ] && { echo "❌ Timeout waiting for client/stream"; exit 1; }
    sleep 1
done

echo "▶ Testing Bitrate switch to 1 Mbps via Control API..."
curl -s -X POST -H "Content-Type: application/json" \
    -d '{"id":"bitrate.set:1"}' \
    "http://127.0.0.1:${CONTROL_PORT}/command" > /dev/null

echo "▶ Verifying stream remains active after switch to 1 Mbps..."
INITIAL_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
sleep 3
CURRENT_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
if [ "$CURRENT_FRAMES" -gt "$INITIAL_FRAMES" ]; then
    echo "✅ Stream active at 1 Mbps ($CURRENT_FRAMES frames)."
else
    echo "❌ FAILURE: Stream frozen after bitrate switch to 1 Mbps."
    exit 1
fi

echo "▶ Testing Bitrate switch to 20 Mbps via Control API..."
curl -s -X POST -H "Content-Type: application/json" \
    -d '{"id":"bitrate.set:20"}' \
    "http://127.0.0.1:${CONTROL_PORT}/command" > /dev/null

echo "▶ Verifying stream remains active after switch to 20 Mbps..."
INITIAL_FRAMES=$CURRENT_FRAMES
sleep 3
CURRENT_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
if [ "$CURRENT_FRAMES" -gt "$INITIAL_FRAMES" ]; then
    echo "✅ Stream active at 20 Mbps ($CURRENT_FRAMES frames)."
else
    echo "❌ FAILURE: Stream frozen after bitrate switch to 20 Mbps."
    exit 1
fi

echo "▶ Checking server logs for encoder recreation..."
if grep -q "Creating new VTEncoder.*bitrate 20000 kbps" "${SERVER_LOG}"; then
    echo "✅ Verified server log shows encoder recreation with 20 Mbps."
else
    echo "❌ FAILURE: Server log does not show expected encoder recreation."
    exit 1
fi

echo "🎉 ALL BITRATE TESTS PASSED!"
exit 0
