#!/usr/bin/env bash
# run-macos-split.sh — Build and run LLrdc in macOS split mode.
set -euo pipefail

# Configuration
IMAGE_NAME="danchitnis/llrdc"
IMAGE_TAG="macos"
CONTAINER_NAME="llrdc-macos"

echo "========================================"
echo "🚀 LLrdc macOS Split Remote Desktop"
echo "========================================"

# Cleanup function for Ctrl+C
cleanup() {
    echo ""
    echo "========================================"
    echo "🛑 Shutting down..."
    
    # Kill the host server if it's running
    if [ -n "${MACOS_SERVER_PID:-}" ]; then
        kill "$MACOS_SERVER_PID" 2>/dev/null || true
    fi
    
    # Also catch any rogue instances
    killall macos-server 2>/dev/null || true
    
    # Stop the Docker container
    docker stop "$CONTAINER_NAME" >/dev/null 2>&1 || true
    docker rm "$CONTAINER_NAME" >/dev/null 2>&1 || true
    
    echo "✅ Cleanup complete. Goodbye!"
    exit 0
}

# Trap SIGINT (Ctrl+C) and SIGTERM
trap cleanup SIGINT SIGTERM

echo "🧹 Cleaning up previous session..."
killall macos-server 2>/dev/null || true
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

echo "📦 Building components..."
npm run build --silent
go build -o macos-server ./server/macos/*.go

echo "🐳 Building/Verifying Docker container..."
./docker-build.sh --macos > /dev/null
docker tag "${IMAGE_NAME}:${IMAGE_TAG}" "${IMAGE_NAME}:latest"

echo "🖥️  Starting macOS Host Server..."
# We run this in the background so we can monitor it and handle cleanup
./macos-server &
MACOS_SERVER_PID=$!

# Wait for ports to bind
echo "⏳ Waiting for host to be ready..."
MAX_RETRIES=5
COUNT=0
while ! lsof -i :8080 >/dev/null || ! lsof -i :12345 >/dev/null; do
    sleep 1
    COUNT=$((COUNT + 1))
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo "❌ ERROR: Host server failed to start on ports 8080/12345."
        cleanup
    fi
done

echo "🔗 Starting Docker Container (Agent Mode)..."
docker run -d \
  --name "${CONTAINER_NAME}" \
  --shm-size=2gb \
  -e PULSE_SERVER=unix:/tmp/pulseaudio.socket \
  -p 12346:12346 \
  -p 12348:12348 \
  --add-host host.docker.internal:host-gateway \
  "${IMAGE_NAME}:${IMAGE_TAG}" > /dev/null

echo "========================================"
echo "✅ SUCCESS! LLrdc is running."
echo "👉 Open: http://localhost:8080/viewer.html"
echo "========================================"
echo "Press Ctrl+C to stop the session."

# Keep the script running to monitor the processes and wait for Ctrl+C
while kill -0 "$MACOS_SERVER_PID" 2>/dev/null; do
    sleep 1
done

# If we get here, the server process died unexpectedly
echo "❌ Host server stopped unexpectedly."
cleanup
