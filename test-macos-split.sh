#!/usr/bin/env bash
# test-macos-split.sh
# Usage: ./test-macos-split.sh [test_file.spec.ts]
# If no argument is provided, runs all tests in tests/macos-browser/

set -euo pipefail

# Configuration
IMAGE_NAME="danchitnis/llrdc"
IMAGE_TAG="macos"
CONTAINER_NAME="llrdc-macos"
LOG_DIR="test-logs"
mkdir -p "$LOG_DIR"

TEST_TO_RUN="${1:-}"

echo "========================================"
echo "Cleaning up previous session..."
killall macos-server 2>/dev/null || true
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
rm -f "$LOG_DIR"/*.log

echo "========================================"
echo "Building components..."
npm run build
go build -o macos-server ./server/macos/*.go

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
  -p 12348:12348 \
  --add-host host.docker.internal:host-gateway \
  "${IMAGE_NAME}:${IMAGE_TAG}" > /dev/null

echo "Waiting for container to connect to host..."
sleep 5
if ! grep -q "Video producer connected" "$LOG_DIR/macos-server.log"; then
    echo "⚠️ Warning: Video producer hasn't connected yet. Check container logs."
    docker logs "$CONTAINER_NAME" | tail -n 20
fi

echo "========================================"
echo "Running Playwright Tests..."
export CONTAINER_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
set +e

if [ -n "$TEST_TO_RUN" ]; then
    echo "Running specific test: $TEST_TO_RUN"
    npx playwright test "tests/macos-browser/$TEST_TO_RUN" --workers=1 --reporter=line --max-failures=1
    TEST_EXIT=$?
else
    echo "Running all macOS tests..."
    npx playwright test tests/macos-browser/ --workers=1 --reporter=line --max-failures=1
    TEST_EXIT=$?
fi
set -e

if [ $TEST_EXIT -eq 0 ]; then
    echo "✅ TEST(S) PASSED"
else
    echo "❌ TEST(S) FAILED"
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
