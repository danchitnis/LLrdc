#!/usr/bin/env bash
set -euo pipefail

# test-linux-full.sh — Comprehensive integration test suite for Linux server.

echo "===================================================="
echo "🚀 Starting Full Linux Integration Test Suite"
echo "===================================================="

# 1. Build the CPU-only container to ensure a fresh state
echo "📦 Building Linux CPU-only Docker image..."
./docker-build.sh --variant cpu

echo ""
echo "----------------------------------------------------"
echo "🔍 Running Playwright Core Tests..."
echo "----------------------------------------------------"

# List of critical Linux tests to run
LINUX_TESTS=(
  "tests/wayland_minimal.spec.ts"      # Basic connectivity and input
  "tests/wayland_all_codecs.spec.ts"  # Codec support (VP8, H264, AV1)
  "tests/wayland_codec_switch.spec.ts" # Dynamic switching
  "tests/wayland_hdpi.spec.ts"         # Scaling and HDPI
  "tests/wayland_framerate.spec.ts"    # Dynamic FPS changes
)

FAILED_TESTS=()

for test_file in "${LINUX_TESTS[@]}"; do
  echo "▶ Executing $test_file..."
  if npx playwright test "$test_file"; then
    echo "✅ $test_file passed."
  else
    echo "❌ $test_file FAILED."
    FAILED_TESTS+=("$test_file")
  fi
  echo ""
done

echo "----------------------------------------------------"
echo "📊 Test Suite Summary"
echo "----------------------------------------------------"

if [ ${#FAILED_TESTS[@]} -eq 0 ]; then
  echo "✅ ALL LINUX TESTS PASSED!"
  exit 0
else
  echo "❌ SOME TESTS FAILED:"
  for failed in "${FAILED_TESTS[@]}"; do
    echo "   - $failed"
  done
  exit 1
fi
