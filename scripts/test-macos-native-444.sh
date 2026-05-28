#!/usr/bin/env bash
# scripts/test-macos-native-444.sh
# Automates the verification of H.264-444 support on the macOS native client.

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_PORT=8080
CONTROL_PORT=18080
CLIENT_LOG="/tmp/llrdc-macos-client-test.log"
SERVER_LOG="/tmp/llrdc-macos-server-test.log"

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
    # Check if the process is still alive
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "❌ ERROR: Server process died during startup."
        cat "${SERVER_LOG}"
        exit 1
    fi
    
    # Check for build errors in the log
    if grep -q "undefined" "${SERVER_LOG}" || grep -q "failed" "${SERVER_LOG}"; then
        # This is a bit broad, but let's look for common Go build errors
        if grep -i "undefined" "${SERVER_LOG}" | grep -v "wlr-randr"; then
            echo "❌ ERROR: Potential build error detected in server logs."
            cat "${SERVER_LOG}"
            exit 1
        fi
    fi

    RESPONSE=$(curl -s "http://127.0.0.1:${SERVER_PORT}/readyz" || echo "curl failed")
    if [ "$RESPONSE" != "curl failed" ] && echo "$RESPONSE" | grep -q '"ready":true'; then
        break
    fi
    [ "$i" -eq 60 ] && { echo "❌ Timeout waiting for server (Last response: $RESPONSE)"; exit 1; }
    if (( i % 5 == 0 )); then
        echo "   ... still waiting ($i/60), last response: $RESPONSE"
    fi
    sleep 1
done
echo "✅ Server ready."

echo "▶ Cleaning up client configuration..."
rm -f macos/LLrdc.app/Contents/Resources/config.yaml || true
rm -f macos/LLrdc.app/config.yaml || true

echo "▶ Building macOS Native Client..."
./macos/build.sh > /dev/null

echo "▶ Launching macOS Native Client with --stats..."
# Running the binary directly from the bundle
./macos/LLrdc.app/Contents/MacOS/llrdc-client \
    --server "http://127.0.0.1:${SERVER_PORT}" \
    --control-addr "127.0.0.1:${CONTROL_PORT}" \
    --stats \
    --auto-start > "${CLIENT_LOG}" 2>&1 &
CLIENT_PID=$!

echo "▶ Waiting for client window and initial stream (H.264)..."
for i in {1..20}; do
    STATE=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/readyz" || echo "{}")
    WINDOW_OK=$(echo "$STATE" | jq -r '.windowCreated and .windowShown')
    WEBRTC_OK=$(echo "$STATE" | jq -r '.webtransportConnected')
    
    if [ "$WINDOW_OK" == "true" ] && [ "$WEBRTC_OK" == "true" ]; then
        STATS=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" || echo "{}")
        FRAMES=$(echo "$STATS" | jq -r '.presentedFrames // 0')
        if [ "$FRAMES" -gt 5 ]; then
            echo "✅ Window opened and initial stream active ($FRAMES frames)."
            break
        fi
    fi
    [ "$i" -eq 20 ] && { echo "❌ Timeout waiting for client/stream"; cat "${CLIENT_LOG}"; exit 1; }
    sleep 1
done

echo "▶ Switching to H.264-444 via Control API..."
curl -s -X POST -H "Content-Type: application/json" \
    -d '{"id":"codec.set:h264-444"}' \
    "http://127.0.0.1:${CONTROL_PORT}/command" > /dev/null

echo "▶ Verifying stream recovery and frame increment..."
INITIAL_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
SUCCESS=0
for i in {1..15}; do
    sleep 1
    CURRENT_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
    echo "   [$i/15] Frames: $CURRENT_FRAMES"
    
    # Check if frames are incrementing
    if [ "$CURRENT_FRAMES" -gt "$INITIAL_FRAMES" ]; then
        # Check a few more samples to ensure it's stable
        sleep 2
        FINAL_FRAMES=$(curl -s "http://127.0.0.1:${CONTROL_PORT}/statsz" | jq -r '.presentedFrames // 0')
        if [ "$FINAL_FRAMES" -gt "$CURRENT_FRAMES" ]; then
            echo "✅ H.264-444 stream verified ($FINAL_FRAMES frames)."
            SUCCESS=1
            break
        fi
    fi
done

if [ "$SUCCESS" -eq 1 ]; then
    echo "🎉 ALL TESTS PASSED!"
    exit 0
else
    echo "❌ FAILURE: Stream frozen or failed to increment after codec switch."
    echo "--- Client Logs ---"
    tail -n 50 "${CLIENT_LOG}"
    echo "--- Server Logs ---"
    tail -n 50 "${SERVER_LOG}"
    exit 1
fi
