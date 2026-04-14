#!/usr/bin/env bash
set -euo pipefail

# This script verifies the native Wayland client in a headed environment.
# It builds the server and client, starts the server, and then runs the client
# 3 times sequentially to ensure everything works correctly in Wayland mode.

echo "▶ Building the server in Docker..."
./docker-build.sh

echo "▶ Building the client in Docker and packaging for host..."
# We ensure the script is executable first
chmod +x ./scripts/package-native-client.sh
./scripts/package-native-client.sh

# Cleanup function to ensure everything is killed on exit
cleanup() {
    echo ""
    echo "▶ Cleaning up..."
    # Kill the client if it's still running
    if [[ -n "${CLIENT_PID:-}" ]]; then
        kill "$CLIENT_PID" 2>/dev/null || true
    fi
    # Stop and remove the server container
    echo "  - Stopping server container..."
    docker rm -f llrdc-headed-server >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "▶ Starting server in Docker with TEST_PATTERN..."
docker run -d --name llrdc-headed-server \
    --network host \
    -e TEST_PATTERN=1 \
    danchitnis/llrdc:latest \
    /app/llrdc --port 8080 --video-codec vp8 >/dev/null
sleep 5

for i in {1..3}; do
    echo ""
    echo "▶ Run #$i: Launching native Wayland client..."
    # Run the client in headed mode directly on the user's desktop.
    # We use --auto-start to bypass physical click requirements since 
    # simulating clicks on native Wayland is often restricted for security.
    ./dist/llrdc-client-linux-amd64/bin/llrdc-client \
        --server http://127.0.0.1:8080 \
        --control-addr 127.0.0.1:18080 \
        --auto-start > client_headed_$i.log 2>&1 &
    CLIENT_PID=$!
    
    # Wait for the client to start and stream to begin
    echo "  - Waiting for stream to initialize..."
    sleep 6
    
    echo "  - Verifying frames..."
    STATS=$(curl -fsS http://127.0.0.1:18080/statsz 2>/dev/null || echo '{"presentedFrames":0}')
    FRAMES=$(echo "$STATS" | grep -Po '"presentedFrames":\K[0-9]+' || echo "0")
    echo "  - Result: $FRAMES frames presented."

    if [ "$FRAMES" -lt 30 ]; then
        echo "❌ FAILED on Run #$i: Stream did not deliver sufficient frames."
        echo "--- SERVER LOGS ---"
        cat server_headed.log
        echo "--- CLIENT LOGS ---"
        cat client_headed_$i.log
        exit 1
    else
        echo "  ✅ Run #$i Success!"
    fi

    echo "  - Closing client..."
    kill "$CLIENT_PID" 2>/dev/null || true
    unset CLIENT_PID
    sleep 2 # Give it a moment to fully close
done

echo ""
echo "🎉 VERIFIED: Headed test passed! Client successfully connected 3 times in a row."
exit 0
