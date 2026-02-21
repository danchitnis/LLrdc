# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder
WORKDIR /app
# Download dependencies first
COPY go.mod go.sum ./
RUN go mod download
# Copy source and build
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -buildvcs=false -o llrdc -ldflags="-w -s" ./cmd/server

FROM ubuntu:24.04

# Avoid interactive prompts during apt installs
ENV DEBIAN_FRONTEND=noninteractive

# ── System dependencies ──────────────────────────────────────────────────────
RUN apt-get update && apt-get install -y --no-install-recommends \
  # X11 / Virtual framebuffer
  xvfb \
  x11-xserver-utils \
  xdotool \
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
  && rm -rf /var/lib/apt/lists/*

# ── Non-root user ────────────────────────────────────────────────────────────
# Create user 'remote' with a home directory and add to sudo group (no password).
# Must come before any step that writes to /home/remote.
RUN useradd -m -s /bin/bash remote \
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
  && chown -R remote:remote /home/remote

# ── ffmpeg static binary (downloaded at build time) ─────────────────────────
# Downloaded before source files so this expensive step is cached independently
# of any code changes. Places ffmpeg at /app/bin/ffmpeg (FFMPEG_PATH).
RUN mkdir -p /app/bin /tmp/ffmpeg-dl \
  && curl -fsSL https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz \
  -o /tmp/ffmpeg.tar.xz \
  && tar -xJf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg-dl --strip-components=1 \
  && mv /tmp/ffmpeg-dl/ffmpeg /app/bin/ffmpeg \
  && chmod +x /app/bin/ffmpeg \
  && rm -rf /tmp/ffmpeg-dl /tmp/ffmpeg.tar.xz

# ── App directory ─────────────────────────────────────────────────────────────
WORKDIR /app

# Copy public assets (these change most frequently)
COPY public/ ./public/

# Copy the compiled Go server binary from the builder stage
COPY --from=builder /app/llrdc /app/llrdc

# ── Housekeeping ──────────────────────────────────────────────────────────────
# Hand /app ownership to 'remote' and switch to that user for runtime.
RUN chown -R remote:remote /app
USER remote

# Expose the WebSocket / HTTP port
EXPOSE 8080

# Graceful-shutdown: forward SIGTERM to go binary
STOPSIGNAL SIGTERM

CMD ["/app/llrdc"]
