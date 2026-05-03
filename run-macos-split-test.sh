#!/usr/bin/env bash
# run-macos-split-test.sh
set -euo pipefail

# Configuration
IMAGE_NAME="danchitnis/llrdc"
IMAGE_TAG="macos"
CONTAINER_NAME="llrdc-macos"
LOG_DIR="test-logs"
mkdir -p "$LOG_DIR"

echo "========================================"
echo "Cleaning up previous session..."
killall macos-server 2>/dev/null || true
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
rm -f "$LOG_DIR"/*.log

echo "========================================"
echo "Building components..."
npm run build
go build -o macos-server ./cmd/macos-server/*.go

echo "========================================"
echo "Building Docker container..."
./docker-build.sh --macos
docker tag "${IMAGE_NAME}:${IMAGE_TAG}" "${IMAGE_NAME}:latest"

echo "========================================"
echo "Starting macos-server..."
# Run with USE_DEBUG_INPUT=true to capture diagnostic logs
export USE_DEBUG_INPUT=true
./macos-server > "$LOG_DIR/macos-server.log" 2>&1 &
MACOS_SERVER_PID=$!

echo "Waiting for macos-server to bind ports..."
MAX_RETRIES=10
COUNT=0
while ! lsof -i :8080 >/dev/null || ! lsof -i :12345 >/dev/null; do
    sleep 1
    COUNT=$((COUNT + 1))
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo "❌ ERROR: macos-server failed to bind ports in time."
        cat "$LOG_DIR/macos-server.log"
        kill $MACOS_SERVER_PID
        exit 1
    fi
done
echo "✅ macos-server is ready."

echo "========================================"
echo "Starting container (detached)..."
docker run -d \
  --name "${CONTAINER_NAME}" \
  --shm-size=2gb \
  -e PULSE_SERVER=unix:/tmp/pulseaudio.socket \
  -e USE_DEBUG_INPUT=true \
  -p 12346:12346 \
  --add-host host.docker.internal:host-gateway \
  "${IMAGE_NAME}:${IMAGE_TAG}" > /dev/null

echo "Waiting for container to connect to host..."
sleep 5
if ! grep -q "Video producer connected" "$LOG_DIR/macos-server.log"; then
    echo "⚠️ Warning: Video producer hasn't connected yet. Check container logs."
    docker logs "$CONTAINER_NAME" | tail -n 20
fi

echo "========================================"
echo "Running Playwright Test..."
export CONTAINER_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
set +e
./test.sh tests/macos_split.spec.ts
TEST_EXIT=$?
set -e

if [ $TEST_EXIT -eq 0 ]; then
    echo "✅ TEST PASSED"
else
    echo "❌ TEST FAILED"
    echo "--- LAST 50 LINES OF macos-server.log ---"
    tail -n 50 "$LOG_DIR/macos-server.log"
    echo "--- LAST 50 LINES OF CONTAINER LOGS ---"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -n 50
fi

echo "========================================"
echo "Tearing down..."
kill $MACOS_SERVER_PID 2>/dev/null || true
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

exit $TEST_EXIT
