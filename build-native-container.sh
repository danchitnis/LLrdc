#!/usr/bin/env bash
# build-native-container.sh — Compile the custom C++ direct capture app inside a container.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

echo "▶ Spin up a Docker container to build nvidia_direct_capture_native..."

# We use standard ubuntu:24.04 image to reproducibly compile the code inside a container
docker run --rm -v "${SCRIPT_DIR}:/workspace" -w /workspace ubuntu:24.04 bash -c "
  set -euo pipefail
  echo '▶ [Container] Updating package list and installing Wayland build tools...'
  apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    pkg-config \
    libwayland-dev \
    libwayland-bin \
    libc6-dev \
    libvulkan-dev
    
  echo '▶ [Container] Generating Wayland protocols...'
  wayland-scanner client-header tools/wayland/wlr-export-dmabuf-unstable-v1.xml /tmp/wlr-export-dmabuf-unstable-v1-client-protocol.h
  wayland-scanner private-code tools/wayland/wlr-export-dmabuf-unstable-v1.xml /tmp/wlr-export-dmabuf-unstable-v1-client-protocol.c
  
  echo '▶ [Container] Compiling the custom CUDA/Wayland capture utility...'
  g++ -O3 -Wall -o cmd/nvidia_direct_capture/nvidia_direct_capture_native \
    tools/wayland/nvidia_direct_capture_native.cpp \
    -I/tmp -Itools/wayland \
    -lwayland-client -lvulkan
    
  echo '▶ [Container] Build succeeded!'
"

echo "✔ Successfully compiled and verified build inside the container."
