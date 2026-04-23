# LLrdc

LLrdc (Low Latency remote desktop) is an entirely web-based, low-latency remote desktop and container solution.

## Features

- **XFCE4 Desktop in Docker**: Runs a full Ubuntu 24.04 and XFCE4 desktop environment inside a reproducible Docker container.
- **Web-Based Client**: Access your desktop entirely via a modern web browser—no client software required.
- **High-Performance Streaming**: Leverages WebRTC for ultra-low latency video streaming, with fallback to WebCodecs/WebSockets. Uses variable bitrate (bitrate drops on static screens) with an optional peak bandwidth cap.
- **Native Go Client**: Includes a Docker-built Linux native client using Go, SDL2, and libvpx with no Chromium, WebView, or WebKit dependency.



## How to Build and Run (using Docker)

LLrdc provides convenient bash scripts to handle all Docker operations easily.

### Prerequisites

- Docker installed

### 1. Build the Docker Image

To build the Docker image, run the included build script from the repository's root directory:

```bash
./docker-build.sh
```

This builds the CPU-only image tagged `danchitnis/llrdc:latest`.

If you want Intel QSV support, build the Intel variant explicitly:

```bash
./docker-build.sh --intel
```

This creates `danchitnis/llrdc:intel`, which includes the Intel media drivers, QSV tooling, and related FFmpeg acceleration stack.

### 2. Run the Container

Once built, start the container using the run script:

```bash
./docker-run.sh
```

To enable GPU acceleration (NVENC) on NVIDIA systems, add the `--nvidia` flag:

```bash
./docker-run.sh --nvidia
```

The script will automatically detect and map CUDA/NVCC paths and switch to `h264_nvenc` encoding for high-performance streaming.

To enable Intel QSV acceleration, build the Intel image first and then run with `--intel`:

```bash
./docker-build.sh --intel
./docker-run.sh --intel
```

When `--intel` is passed, `docker-run.sh` automatically targets the `:intel` image tag unless `IMAGE_TAG` is explicitly set. If you force `IMAGE_TAG=latest`, the script will fail fast because `:latest` is now the CPU-only image.

To request the new GPU direct-buffer path, use `--capture-mode direct` together with `--nvidia`:

```bash
./docker-run.sh --nvidia --capture-mode direct
```

This mode is fail-closed: startup aborts unless the compositor exposes the required Wayland screencopy and linux-dmabuf capabilities.

To see verbose debug logs, you can use the following flags:
- `--debug-ffmpeg`: Shows real-time ffmpeg frame rate and encoder reports.
- `--debug`: Enables both ffmpeg and input debug logging.
- `--hdpi [percent]` or `-h [percent]`: Enables High DPI scaling for the XFCE desktop. If no percentage is provided, it defaults to `200` (2x scaling). Example: `--hdpi 150` for 1.5x scaling.
- `--capture-mode compat|direct`: Selects the Wayland capture path. `direct` requires `--nvidia` or `--intel` and only activates when direct-buffer probing succeeds.
- `--webrtc-buffer [frames]`: Sets the WebRTC frame buffer limit (default: `30`).
- `--activity-hz [hz]`: Sets the input activity heartbeat frequency (default: `30`).
- `--activity-timeout [ms]`: Sets how long the heartbeat continues after last input (default: `1500`).
- `--no-nvenc-latency`: Disables ultra-low-latency NVENC optimizations.

### Network and WebRTC Configuration

By default, the server auto-detects your primary IP address for WebRTC. If you have multiple network interfaces (e.g., Tailscale, VPNs, or multiple LANs), you can use the following flags to control which interfaces are used for the stream:

- `--iface <name>` or `-i <name>`: Prefer the specified host interface when selecting the public WebRTC IP (for Docker bridge mode, this does not map directly to container NIC names such as `eth0`).
- `--exclude-iface <name>` or `-x <name>`: Prevent WebRTC from using the specified interface (e.g., `tailscale0`).

```bash
# Example: Exclude Tailscale and use real IP
./docker-run.sh -x tailscale0
```

### 3. Connect

Open your browser and navigate to:

```
http://localhost:8080
```

You should see your XFCE4 desktop session running and ready for interaction.

## Native Client

The repo also includes a native Linux client in [cmd/client/main.go](/home/danial/code/LLrdc/cmd/client/main.go). It is built and tested inside Docker from [Dockerfile.client](/home/danial/code/LLrdc/Dockerfile.client), but runs as a real SDL windowed client rather than embedding Chromium, WebView, or WebKit.

### Build the Native Client Image

```bash
npm run client:build
```

### Package the Native Host Client

The intended delivery model is: build and package in Docker, then run the produced binary directly on the Linux host.

```bash
npm run client:package
```

That creates:

```text
dist/llrdc-client-linux-amd64/
dist/llrdc-client-linux-amd64.tar.gz
```

The package includes:
- `bin/llrdc-client`: host launcher
- `bin/llrdc-client.bin`: packaged client binary
- `bin/linux-uinput-bench`: packaged native latency injector launcher
- `bin/linux-uinput-bench.bin`: packaged native latency injector binary
- `lib/`: bundled runtime shared libraries

### Run the Native Client on the Host

Run the packaged client directly from the host filesystem:

```bash
./dist/llrdc-client-linux-amd64/bin/llrdc-client \
  --server http://127.0.0.1:8080 \
  --control-addr 127.0.0.1:18080
```

Or use the wrapper script, which packages in Docker first if the host bundle is missing:

```bash
./scripts/run-native-client.sh
```

Force a rebuild/package before launch:

```bash
./scripts/run-native-client.sh --rebuild
```

Important flags:
- `--server`: Existing LLrdc server URL. The server protocol is unchanged.
- `--control-addr`: Loopback/API bind address for health checks, hooks, and automation.
- `--width`, `--height`, `--title`: Initial native window sizing.
- `--exit-after`: Auto-exit for smoke tests.
- `--headless`: Disables the window; intended only for debugging, not normal native-client use.

Display backend behavior:
- Native Wayland is preferred automatically when a Wayland socket is available.
- X11/Xwayland remains available with `SDL_VIDEODRIVER=x11`.
- X11 is selected automatically only when Wayland is unavailable and `DISPLAY` is present.
- No Chromium, WebView, or WebKit is used in any path.

### Verify the Packaged Host Runtime

This runs the packaged client directly on the host in `--headless` mode to verify the exported binary and bundled libraries work outside Docker:

```bash
npm run client:verify-package
```

### Docker Runtime Smoke Mode

### Test the Native Client

The native client has Dockerized unit tests plus a windowed smoke test:

```bash
npm run client:test
```

That command:
- runs `go test -tags native ./internal/client ./cmd/client` in the Docker `test` stage
- builds the runtime image
- launches the windowed client against an in-container Xvfb display
- verifies the control API comes up on `/statez`, `/readyz`, and `/latencyz/latest`

There is also an end-to-end Docker test against the unchanged LLrdc server:

```bash
npm run client:test:e2e
```

That test starts the existing server container in `--test-pattern` mode, connects the native client to it over WebRTC on a private Docker network, and waits for `/statez` and `/statsz` to show an active stream with decoded video frames.

For headed host-desktop validation on Linux:

```bash
npm run client:test:headed
npm run client:benchmark:latency
```

- `client:test:headed` is a Wayland-first visual smoke test. It packages the client, runs a short Wayland SDL capability probe, connects to a test-pattern server, and captures `/snapshotz` plus a compositor screenshot when supported.
- `client:benchmark:latency` is the official native Linux latency lane. It packages the client, launches a dedicated Weston bench, drives deterministic pointer moves through the native client control API, and reports stage timings from control injection through native present using a monotonic clock plus probe-marker correlation.

## Clipboard

LLrdc supports bidirectional clipboard synchronization between the host browser and the remote desktop.

### Copy & Paste from Host to Remote

- **Keyboard shortcut (Cmd+V / Ctrl+V)**: Works immediately. When you paste, the browser captures the clipboard text, sends it to the remote, and injects Ctrl+V into the active remote application.
- **Context menu paste in remote apps**: After pasting once via Cmd+V, the remote clipboard is synced. Subsequent context menu paste operations in remote applications (e.g., mousepad → right-click → Paste) will use the synced text.

### Copy from Remote to Host

Text copied in the remote desktop (e.g., via Ctrl+C in a terminal) is automatically synced to the host browser clipboard within ~1 second.

### Disabling Clipboard

Clipboard synchronization can be disabled if it impacts performance or is not needed:

- **At runtime**: Uncheck "Enable Clipboard Sync" in the config panel (Input tab).
- **At startup**: Set `ENABLE_CLIPBOARD=false` or use `--enable-clipboard=false`.

When disabled, all clipboard polling, sync, and focus management are turned off.

## Configuration Options

LLrdc can be configured using command-line flags (when running the binary directly in a custom container) or environment variables (when using `docker-run.sh`).

### Command-Line Flags

The `llrdc` binary supports the following flags, categorized by their primary use case:

#### User Flags
- `--port`: Port for both HTTP and WebRTC UDP (default: `8080`).
- `--fps`: Target frames per second (default: `30`).
- `--video-codec`: Choice of `vp8` (default), `h264`, `h264_nvenc`, `h264_qsv`, `h265`, `h265_nvenc`, `h265_qsv`, `av1`, `av1_nvenc`, or `av1_qsv`.
- `--chroma`: Chroma subsampling format, `420` (default) or `444`. See [Chroma 4:4:4](#chroma-444) below.
- `--use-nvidia`: Enable NVIDIA acceleration for NVENC codecs.
- `--use-intel`: Enable Intel acceleration for QSV codecs.
- `--capture-mode`: Capture mode, `compat` (default) or `direct`.
- `--use-debug-ffmpeg`: Enable verbose FFmpeg logging.
- `--use-debug-x11`: Enable verbose X11/XFCE session logging.
- `--display-num`: X11 display number inside the container (default: `99`).
- `--wallpaper`: Path to a custom wallpaper image.
- `--webrtc-public-ip`: Manually set the public IP for ICE candidates.
- `--webrtc-interfaces`: Comma-separated allowlist of network interfaces.
- `--webrtc-exclude-interfaces`: Comma-separated blocklist of network interfaces.
- `--webrtc-buffer`: WebRTC frame channel size (default: `30`). Lower values reduce lag but may increase stutter.
- `--activity-hz`: Input heartbeat frequency in Hz (default: `30`). Controls how often the server pings for damage during movement.
- `--activity-timeout`: Inactivity timeout in ms before stopping the heartbeat (default: `1500`).
- `--nvenc-latency`: Enable ultra-low latency NVENC optimizations (default: `true`).
- `--enable-clipboard`: Enable clipboard synchronization (default: `true`).

#### Testing Flags
- `--test-pattern`: Run with an FFmpeg `testsrc` pattern instead of capturing the Wayland session.

### Environment Variables

When using `docker-run.sh`, you can pass these environment variables to override defaults:

```bash
PORT=9090 HOST_PORT=9090 FPS=60 VIDEO_CODEC=h264 ./docker-run.sh
```

| Variable | Description | Flag Equivalent |
| :--- | :--- | :--- |
| `PORT` | Server internal port | `--port` |
| `FPS` | Target frames per second | `--fps` |
| `VIDEO_CODEC` | Encoder selection | `--video-codec` |
| `CHROMA` | Chroma subsampling (`420` or `444`) | `--chroma` |
| `USE_NVIDIA` | Enable NVIDIA acceleration | `--use-nvidia` |
| `USE_INTEL` | Enable Intel acceleration | `--use-intel` |
| `CAPTURE_MODE` | Capture mode (`compat` or `direct`) | `--capture-mode` |
| `USE_DEBUG_FFMPEG` | Enable FFmpeg debug logs | `--use-debug-ffmpeg` |
| `USE_DEBUG_X11` | Enable X11 debug logs | `--use-debug-x11` |
| `WEBRTC_PUBLIC_IP` | Public IP override | `--webrtc-public-ip` |
| `WEBRTC_BUFFER_SIZE` | WebRTC frame channel size | `--webrtc-buffer` |
| `ACTIVITY_PULSE_HZ` | Heartbeat frequency (Hz) | `--activity-hz` |
| `ACTIVITY_TIMEOUT` | Inactivity timeout (ms) | `--activity-timeout` |
| `NVENC_LATENCY_MODE` | Toggle NVENC ULL (Ultra Low Latency) | `--nvenc-latency` |
| `TEST_PATTERN` | Use FFmpeg test pattern | `--test-pattern` |
| `TEST_MINIMAL_X11` | Skip XFCE startup | `--test-minimal-x11` |
| `WALLPAPER` | Custom wallpaper path | `--wallpaper` |
| `ENABLE_CLIPBOARD` | Enable clipboard sync | `--enable-clipboard` |

## Reproducible Benchmarks

The repo now uses two canonical latency benchmark scenarios:

1. `low-end CPU`: `vp8`, `1080p`, `compat`, at `30 FPS` and `60 FPS`
2. `high-end GPU`: `av1_nvenc`, `4K`, `compat` vs `direct`, at `60 FPS`

The benchmark harness is in [tests/wayland_latency_breakdown.spec.ts](/home/danial/code/LLrdc/tests/wayland_latency_breakdown.spec.ts), and the canonical results live in [latency-breakdown.md](/home/danial/code/LLrdc/latency-breakdown.md).

Build first:

```bash
./docker-build.sh
./docker-build.sh --intel
```

Run one profile:

```bash
npm run test:latency:cpu-1080p30
npm run test:latency:cpu-1080p60
npm run test:latency:nvidia-4k60
```

Run the full profile matrix:

```bash
npm run test:latency:profiles
```

The stage-breakdown benchmark combines:
- remote app timestamps from the probe window
- server-side `firstFrameBroadcastAtMs` from `/latencyz`
- browser `requestVideoFrameCallback()` metadata for receive, decode, and presentation timing

The JSON output includes:
- `inputToRequest`
- `requestToDraw`
- `drawToFirstFrameBroadcast`
- `firstFrameBroadcastToReceive`
- `receiveToDecodeReady`
- `decodeReadyToCompose`
- `composeToExpectedDisplay`
- `expectedDisplayToCallback`
- `drawToCallback`
- `inputToCallback`

Notes:
- `4K60` GPU runs require a working local Docker + NVIDIA setup.
- The benchmark disables VBR for reproducibility.
- The `4K60` AV1 profile has an occasional visual-probe flake; the test retries automatically.
- The UI latency bar is not the source of truth for these measurements.

### Legacy Benchmarks

Older ad hoc latency and direct-buffer benchmark scripts are still present:

```bash
npm run test:latency
npm run test:direct-benchmark
```

Use the profile-based commands above for current baseline work.

## Chroma 4:4:4

Chroma 4:4:4 avoids chroma subsampling, improving clarity for text and sharp edges on remote desktops. It can be toggled at runtime from the config panel (Quality tab) or set at startup with `--chroma 444`.

### Codec Support

| Codec | 4:4:4 Support | Notes |
| :--- | :--- | :--- |
| `h264` (CPU) | ✅ | Uses `high444` profile |
| `h264_nvenc` (GPU) | ✅ | Uses `high444p` profile. CPU usage increases (~50-85%) due to required CPU-side BGR→YUV444p conversion before GPU upload |
| `h265` (CPU) | ✅ | Uses `main444-8` profile |
| `h265_nvenc` (GPU) | ✅ | Uses `rext` profile. CPU usage increases due to CPU-side conversion. |
| `av1` (CPU) | ✅ | Uses `libaom-av1` |
| `av1_nvenc` (GPU) | ❌ | NVIDIA NVENC SDK does not support AV1 4:4:4 encoding on any current GPU architecture |
| `vp8` | ❌ | VP8 does not support 4:4:4 |

> **Note:** When using `h264_nvenc` or `h265_nvenc` with chroma 444, CPU usage increases because FFmpeg must convert frames from BGR0 to YUV444p on the CPU before uploading to the GPU. NVIDIA's `scale_cuda` filter does not support this conversion.
from BGR0 to YUV444p on the CPU before uploading to the GPU. NVIDIA's `scale_cuda` filter does not support this conversion.
ding to the GPU. NVIDIA's `scale_cuda` filter does not support this conversion.
