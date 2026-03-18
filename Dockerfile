# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder
WORKDIR /app
# Download dependencies first
COPY go.mod go.sum ./
RUN go mod download
# Copy source and build
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -buildvcs=false -o llrdc -ldflags="-w -s" ./cmd/server

FROM node:22-alpine AS node-builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# ── FFmpeg Custom Builder ──────────────────────────────────────────────────
# Pull latest stable FFmpeg static binaries that INCLUDE nvenc support
FROM alpine:latest AS ffmpeg-builder
RUN apk add --no-cache curl tar xz
WORKDIR /tmp
# Using a build that specifically includes NVENC support
RUN curl -L https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl.tar.xz | tar xJ --strip-components=1

FROM nvidia/cuda:12.8.0-runtime-ubuntu24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV NO_AT_BRIDGE=1
ENV GTK_A11Y=none
ENV GVFS_DISABLE_FUSE=1
ENV LIBOVERLAY_SCROLLBAR=0

# ── System dependencies ──────────────────────────────────────────────────────
RUN apt-get update && apt-get install -y --no-install-recommends \
  # X11 / Virtual framebuffer
  xvfb \
  x11-xserver-utils \
  x11-apps \
  xdotool \
  xautomation \
  xclip \
  # XFCE desktop environment + goodies
  xfce4 \
  xfce4-goodies \
  xfce4-terminal \
  xfce4-notifyd \
  xfce4-taskmanager \
  xfce4-screenshooter \
  xfce4-whiskermenu-plugin \
  xfdesktop4 \
  dbus-x11 \
  # Core session components
  xfce4-session \
  xfce4-panel \
  xfwm4 \
  thunar \
  # Mouse cursor themes (fixes missing/blank cursor)
  dmz-cursor-theme \
  xcursor-themes \
  # Icon themes (matches host)
  adwaita-icon-theme \
  elementary-xfce-icon-theme \
  humanity-icon-theme \
  hicolor-icon-theme \
  tango-icon-theme \
  # GTK themes (Greybird window decorations + GTK2 engines)
  greybird-gtk-theme \
  gnome-themes-extra \
  gtk2-engines-murrine \
  gtk2-engines-pixbuf \
  # SVG rendering for wallpapers (without this, SVG wallpapers show as solid colour)
  librsvg2-common \
  # Misc system tools
  ca-certificates \
  curl \
  xz-utils \
  gnupg \
  sudo \
  # Audio
  pulseaudio \
  alsa-utils \
  # libgl1 is often needed for some ffmpeg hwaccel paths
  libgl1 \
  # Need to install dependencies for the custom ffmpeg build
  libva-drm2 \
  libva-x11-2 \
  vdpau-driver-all \
  && rm -rf /var/lib/apt/lists/*

# ── FFmpeg Installation ──────────────────────────────────────────────────────
# Copy custom ffmpeg and ffprobe from builder stage
COPY --from=ffmpeg-builder /tmp/bin/ffmpeg /usr/local/bin/
COPY --from=ffmpeg-builder /tmp/bin/ffprobe /usr/local/bin/

# ── Browser Repositories (Firefox & Chromium without Snap) ───────────────────
# Allow users to install Firefox and Chromium via apt without snap.
# Snap does not work in unprivileged Docker containers.
RUN apt-get update && apt-get install -y --no-install-recommends software-properties-common \
  && add-apt-repository -y ppa:mozillateam/ppa \
  && add-apt-repository -y ppa:xtradeb/apps \
  && printf 'Package: *\nPin: release o=LP-PPA-mozillateam\nPin-Priority: 1001\n' > /etc/apt/preferences.d/mozilla-firefox \
  && printf 'Unattended-Upgrade::Allowed-Origins:: "LP-PPA-mozillateam:${distro_codename}";\n' > /etc/apt/apt.conf.d/51unattended-upgrades-firefox \
  && printf 'Package: snapd chromium-browser\nPin: release *\nPin-Priority: -1\n' > /etc/apt/preferences.d/nosnap \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

# ── Silencing XFCE warnings ───────────────────────────────────────────────────
# Create a dummy pm-is-supported script to prevent xfce4-session from complaining.
RUN printf '#!/bin/sh\nexit 1\n' > /usr/bin/pm-is-supported \
  && chmod +x /usr/bin/pm-is-supported

# Create X11 and ICE socket directories to prevent permission errors
RUN mkdir -p /tmp/.X11-unix /tmp/.ICE-unix \
  && chmod 1777 /tmp/.X11-unix /tmp/.ICE-unix

# Silence xkbcomp warnings
RUN mv /usr/bin/xkbcomp /usr/bin/xkbcomp.real \
  && printf '#!/bin/sh\nexec /usr/bin/xkbcomp.real -w 0 "$@"\n' > /usr/bin/xkbcomp \
  && chmod +x /usr/bin/xkbcomp

# ── Non-root user ────────────────────────────────────────────────────────────
# Create user 'remote' with a home directory and add to sudo group (no password).
# Ubuntu 24.04 includes a default 'ubuntu' user at UID 1000. We remove it to reuse UID 1000.
# Must come before any step that writes to /home/remote.
ARG UID=1000
RUN userdel -r ubuntu || true \
  && useradd -m -s /bin/bash -u ${UID} remote \
  && echo 'remote ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/remote \
  && chmod 0440 /etc/sudoers.d/remote

# ── Cursor theme default ──────────────────────────────────────────────────────
# Point the X11 "default" icon directory at DMZ-White so every app picks up
# the classic white-background arrow cursor automatically.
RUN mkdir -p /usr/share/icons/default \
  && printf '[Icon Theme]\nInherits=DMZ-White\n' \
  > /usr/share/icons/default/index.theme \
  && echo 'Xcursor.theme: DMZ-White' >> /home/remote/.Xresources \
  && echo 'Xcursor.size: 24'         >> /home/remote/.Xresources \
  && mkdir -p /home/remote/.config/xfce4/xfconf/xfce-perchannel-xml \
  && printf '%s\n' \
  '<?xml version="1.0" encoding="UTF-8"?>' \
  '<channel name="xsettings" version="1.0">' \
  '  <property name="Gtk" type="empty">' \
  '    <property name="CursorThemeName" type="string" value="DMZ-White"/>' \
  '    <property name="CursorThemeSize" type="int"    value="24"/>' \
  '  </property>' \
  '</channel>' \
  > /home/remote/.config/xfce4/xfconf/xfce-perchannel-xml/xsettings.xml \
  && printf '%s\n' \
  '<?xml version="1.0" encoding="UTF-8"?>' \
  '<channel name="xfce4-desktop" version="1.0">' \
  '  <property name="desktop-icons" type="empty">' \
  '    <property name="show-thumbnails" type="bool" value="false"/>' \
  '  </property>' \
  '</channel>' \
  > /home/remote/.config/xfce4/xfconf/xfce-perchannel-xml/xfce4-desktop.xml \
  && chown -R remote:remote /home/remote

# ── App directory ─────────────────────────────────────────────────────────────
WORKDIR /app

# Copy public assets from node builder
COPY --from=node-builder /app/public/ ./public/

# Copy the compiled Go server binary from the builder stage
COPY --from=builder /app/llrdc /app/llrdc

# ── Housekeeping ──────────────────────────────────────────────────────────────
# Ensure app ownership and add entrypoint script
RUN chown -R remote:remote /app
COPY docker-entrypoint.sh /usr/local/bin/
RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh && chmod +x /usr/local/bin/docker-entrypoint.sh

# Expose the WebSocket / HTTP port
EXPOSE 8080

# Graceful-shutdown: forward SIGTERM to go binary
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/llrdc"]
