#!/usr/bin/env bash
# benchmark-macos-remote.sh
# Benchmarks the macOS client against a remote LLrdc server.
# Usage: ./benchmark-macos-remote.sh <server_ip> <port>

set -euo pipefail

SERVER_IP="${1:-192.168.1.21}"
PORT="${2:-8080}"
SERVER_URL="http://${SERVER_IP}:${PORT}"
CONTROL_URL="http://127.0.0.1:18080" # Default macOS control port

echo "Starting benchmark against ${SERVER_URL}..."

# 1. Check if client is reachable
if ! curl -fsS "${CONTROL_URL}/readyz" >/dev/null 2>&1; then
    echo "Error: macOS client control server not found at ${CONTROL_URL}"
    exit 1
fi

# 2. Start Benchmark Sequence
echo "▶ Running benchmark sequence..."

# Collect samples from /latencyz/latest
# Adapt probe logic to query the server-side API or client's latency endpoint
# For now, collect 5 samples
for i in {1..5}; do
    SAMPLE=$(curl -fsS "${CONTROL_URL}/latencyz/latest")
    echo "Sample $i: $SAMPLE"
    sleep 1
done

echo "Benchmark complete."
