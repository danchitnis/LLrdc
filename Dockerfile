# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -buildvcs=false -o llrdc -ldflags="-w -s" ./cmd/server

FROM node:22-alpine AS node-builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM ubuntu:25.04
ENV DEBIAN_FRONTEND=noninteractive
ENV USE_WAYLAND=true

RUN apt-get update && apt-get install -y --no-install-recommends \
  labwc \
  xwayland \
  wlr-randr \
  wlrctl \
  wl-clipboard \
  x11-utils \
  dbus-x11 \
  wf-recorder \
  swaybg \
  ffmpeg \
  xfce4 \
  xfce4-goodies \
  xfce4-pulseaudio-plugin \
  pavucontrol \
  python3 \
  python3-tk \
  adwaita-icon-theme-full \
  elementary-xfce-icon-theme \
  gnome-themes-extra \
  hicolor-icon-theme \
  libpulse0 \
  libegl1 \
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
  gcc \
  libc6-dev \
  libwayland-dev \
  libxkbcommon-dev \
  wayland-protocols \
  libwayland-bin \
  pkg-config \
  && rm -rf /var/lib/apt/lists/*

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
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

ARG UID=1000
RUN userdel -r ubuntu || true \
  && useradd -m -s /bin/bash -u ${UID} remote \
  && echo 'remote ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/remote \
  && chmod 0440 /etc/sudoers.d/remote

WORKDIR /app
COPY --from=node-builder /app/public/ ./public/
COPY --from=builder /app/llrdc /app/llrdc
COPY cmd/server/wayland_input_client.c ./
COPY tools/direct_buffer_probe.c ./
COPY tools/latency_probe_app.py ./tools/
COPY cmd/server/wlr-virtual-pointer-unstable-v1.xml ./
COPY cmd/server/virtual-keyboard-unstable-v1.xml ./

# Generate protocols and build helper
RUN wayland-scanner client-header wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-client-protocol.h \
    && wayland-scanner private-code wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-client-protocol.c \
    && wayland-scanner client-header virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.h \
    && wayland-scanner private-code virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.c \
    && gcc -o wayland_input_client wayland_input_client.c wlr-virtual-pointer-unstable-v1-client-protocol.c virtual-keyboard-unstable-v1-client-protocol.c $(pkg-config --cflags --libs wayland-client xkbcommon) \
    && gcc -O2 -o /usr/local/bin/direct_buffer_probe direct_buffer_probe.c $(pkg-config --cflags --libs wayland-client)

RUN chown -R remote:remote /app
COPY docker-entrypoint.sh /usr/local/bin/
RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh && chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/llrdc"]
