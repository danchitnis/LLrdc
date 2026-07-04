# syntax=docker/dockerfile:1
# Force rebuild to compile direct capture fallback changes
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY server/ ./server/
RUN CGO_ENABLED=0 go build -buildvcs=false -o llrdc -ldflags="-w -s" ./cmd/server \
    && CGO_ENABLED=0 go build -buildvcs=false -o nvidia_direct_capture -ldflags="-w -s" ./cmd/nvidia_direct_capture

FROM node:22-alpine AS node-builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM ubuntu:26.04 AS helper-builder
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
  gcc \
  g++ \
  libc6-dev \
  libwayland-dev \
  libxkbcommon-dev \
  wayland-protocols \
  libwayland-bin \
  pkg-config \
  libvulkan-dev \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY tools/ ./tools/
COPY tools/wayland/nvidia_direct_capture_native.cpp ./

RUN wayland-scanner client-header tools/wayland/wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-client-protocol.h \
    && wayland-scanner private-code tools/wayland/wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-client-protocol.c \
    && wayland-scanner client-header tools/wayland/virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.h \
    && wayland-scanner private-code tools/wayland/virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.c \
    && wayland-scanner client-header tools/wayland/wlr-export-dmabuf-unstable-v1.xml /tmp/wlr-export-dmabuf-unstable-v1-client-protocol.h \
    && wayland-scanner private-code tools/wayland/wlr-export-dmabuf-unstable-v1.xml /tmp/wlr-export-dmabuf-unstable-v1-client-protocol.c \
    && wayland-scanner client-header /usr/share/wayland-protocols/stable/xdg-shell/xdg-shell.xml xdg-shell-client-protocol.h \
    && wayland-scanner private-code /usr/share/wayland-protocols/stable/xdg-shell/xdg-shell.xml xdg-shell-client-protocol.c \
    && gcc -o wayland_input_client tools/wayland/wayland_input_client.c wlr-virtual-pointer-unstable-v1-client-protocol.c virtual-keyboard-unstable-v1-client-protocol.c -I. $(pkg-config --cflags --libs wayland-client xkbcommon) \
    && gcc -O2 -o direct_buffer_probe tools/direct_buffer_probe.c $(pkg-config --cflags --libs wayland-client) \
    && gcc -O2 -o latency_probe tools/latency_probe.c xdg-shell-client-protocol.c -I. $(pkg-config --cflags --libs wayland-client wayland-cursor)

ARG CACHEBUST=11
RUN echo "Cachebust: $CACHEBUST" \
    && g++ -O3 -Wall -o nvidia_direct_capture_native nvidia_direct_capture_native.cpp -I/tmp -Itools/wayland $(pkg-config --cflags --libs wayland-client) -lvulkan

FROM ubuntu:26.04
ARG ENABLE_INTEL=false
ARG BUILD_VARIANT=cpu
LABEL com.danchitnis.llrdc.build-variant="${BUILD_VARIANT}"
ENV DEBIAN_FRONTEND=noninteractive
ENV USE_WAYLAND=true
ENV LLRDC_BUILD_VARIANT="${BUILD_VARIANT}"

RUN apt-get update && apt-get install -y --no-install-recommends \
  labwc \
  xwayland \
  wlr-randr \
  wlrctl \
  wl-clipboard \
  x11-utils \
  dbus-x11 \
  wf-recorder \
  nvidia-vaapi-driver \
  libnvidia-egl-wayland1 \
  libnvidia-egl-gbm1 \
  swaybg \
  ffmpeg \
  xfce4 \
  xfce4-goodies \
  xfce4-pulseaudio-plugin \
  pavucontrol \
  python3 \
  python3-tk \
  glycin-loaders \
  adwaita-icon-theme-full \
  elementary-xfce-icon-theme \
  gnome-themes-extra \
  hicolor-icon-theme \
  libpulse0 \
  libegl1 \
  libvulkan1 \
  shared-mime-info \
  libgbm1 \
  libdrm2 \
  pulseaudio \
  pulseaudio-utils \
  alsa-utils \
  libasound2-plugins \
  librsvg2-common \
  coreutils \
  ca-certificates \
  sudo \
  && rm -rf /var/lib/apt/lists/*

RUN if [ "${ENABLE_INTEL}" = "true" ]; then \
    apt-get update \
    && apt-get install -y --no-install-recommends \
      intel-gpu-tools \
      intel-media-va-driver-non-free \
      libvpl2 \
      libvpl-tools \
      libmfx-gen1.2 \
      va-driver-all \
      libva-drm2 \
      libva2 \
      vainfo \
    && rm -rf /var/lib/apt/lists/*; \
  else \
    rm -rf /var/lib/apt/lists/*; \
  fi

# ── Browser Repositories (Firefox via Official Mozilla APT) ──────────────────
# Allow users to install Firefox via apt without snap.
# Snap does not work in unprivileged Docker containers.
# We use the official Mozilla repo because the mozillateam PPA does not yet support 25.04.
RUN apt-get update && apt-get install -y --no-install-recommends wget apt-transport-https ca-certificates \
  && install -d -m 0755 /etc/apt/keyrings \
  && wget -q https://packages.mozilla.org/apt/repo-signing-key.gpg -O- | tee /etc/apt/keyrings/packages.mozilla.org.asc > /dev/null \
  && echo 'deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main' | tee -a /etc/apt/sources.list.d/mozilla.list > /dev/null \
  && printf 'Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n' > /etc/apt/preferences.d/mozilla \
  && printf 'Package: snapd\nPin: release *\nPin-Priority: -1\n' > /etc/apt/preferences.d/nosnap \
  && apt-get update && apt-get install -y --no-install-recommends firefox \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

ARG UID=1000
RUN userdel -r ubuntu || true \
  && useradd -m -s /bin/bash -u ${UID} remote \
  && echo 'remote ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/remote \
  && if [ "${ENABLE_INTEL}" = "true" ]; then echo 'remote ALL=(ALL) NOPASSWD: /usr/bin/intel_gpu_top' >> /etc/sudoers.d/remote; fi \
  && chmod 0440 /etc/sudoers.d/remote

RUN printf '%s\n' "${BUILD_VARIANT}" > /etc/llrdc-build-variant

WORKDIR /app
COPY --from=node-builder /app/public/ ./public/
COPY --from=builder /app/llrdc /app/llrdc
COPY --from=builder /app/nvidia_direct_capture /usr/local/bin/nvidia_direct_capture
ARG CACHEBUST=11
RUN echo "Cachebust: $CACHEBUST"
COPY --from=helper-builder /build/wayland_input_client ./wayland_input_client
COPY --from=helper-builder /build/direct_buffer_probe /usr/local/bin/direct_buffer_probe
COPY --from=helper-builder /build/latency_probe /usr/local/bin/latency_probe
COPY --from=helper-builder /build/nvidia_direct_capture_native /usr/local/bin/nvidia_direct_capture_native
COPY tools/latency_probe_app.py ./tools/

# Configure GLVND EGL vendor registry and EGL external platform registries for NVIDIA driver compatibility inside the container
RUN mkdir -p /usr/share/glvnd/egl_vendor.d /usr/share/egl/egl_external_platform.d \
    && echo '{"file_format_version":"1.0.0","ICD":{"library_path":"libEGL_nvidia.so.0"}}' > /usr/share/glvnd/egl_vendor.d/10_nvidia.json \
    && echo '{"file_format_version":"1.0.0","ICD":{"library_path":"libnvidia-egl-wayland.so.1"}}' > /usr/share/egl/egl_external_platform.d/20_nvidia_wayland.json

# Trick glycin into disabling sandboxing by pretending to be a Flatpak Devel environment.
# This avoids the need for bwrap/unprivileged namespaces which are blocked by host kernels.
RUN printf '[Instance]\nbuild=true\n\n[Application]\nname=org.llrdc.Devel\n' > /.flatpak-info

RUN chown -R remote:remote /app
COPY docker-entrypoint.sh /usr/local/bin/
RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh && chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/llrdc"]
