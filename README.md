# LLrdc

LLrdc (Low Latency remote desktop) is an entirely web-based, low-latency remote desktop and container solution.

## Features

- **XFCE4 Desktop in Docker**: Runs a full Ubuntu 24.04 and XFCE4 desktop environment inside a reproducible Docker container.
- **Web-Based Client**: Access your desktop entirely via a modern web browser—no client software required.
- **High-Performance Streaming**: Leverages WebRTC for ultra-low latency video streaming, with fallback to WebCodecs/WebSockets. Uses variable bitrate (bitrate drops on static screens) with an optional peak bandwidth cap.



## How to Build and Run (using Docker)

LLrdc provides convenient bash scripts to handle all Docker operations easily.

### Prerequisites

- Docker installed

### 1. Build the Docker Image

To build the Docker image, run the included build script from the repository's root directory:

```bash
./docker-build.sh
```

This will automatically create a Docker image tagged `danchitnis/llrdc:latest`. The build process compiles the Go backend and configures the X11/XFCE environment.

### 2. Run the Container

Once built, start the container using the run script:

```bash
./docker-run.sh
```

To enable GPU acceleration (NVENC) on NVIDIA systems, add the `--gpu` flag:

```bash
./docker-run.sh --gpu
```

The script will automatically detect and map CUDA/NVCC paths and switch to `h264_nvenc` encoding for high-performance streaming.

To request the new GPU direct-buffer path, use `--capture-mode direct` together with `--gpu`:

```bash
./docker-run.sh --gpu --capture-mode direct
```

This mode is fail-closed: startup aborts unless the compositor exposes the required Wayland screencopy and linux-dmabuf capabilities.

To see verbose debug logs, you can use the following flags:
- `--debug-ffmpeg`: Shows real-time ffmpeg frame rate and encoder reports.
- `--debug`: Enables both ffmpeg and input debug logging.
- `--hdpi [percent]` or `-h [percent]`: Enables High DPI scaling for the XFCE desktop. If no percentage is provided, it defaults to `200` (2x scaling). Example: `--hdpi 150` for 1.5x scaling.
- `--capture-mode compat|direct`: Selects the Wayland capture path. `direct` requires `--gpu` and only activates when direct-buffer probing succeeds.
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
- `--video-codec`: Choice of `vp8` (default), `h264`, `h264_nvenc`, `h265`, `h265_nvenc`, `av1`, or `av1_nvenc`.
- `--chroma`: Chroma subsampling format, `420` (default) or `444`. See [Chroma 4:4:4](#chroma-444) below.
- `--use-gpu`: Enable GPU acceleration for NVENC codecs.
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
| `USE_GPU` | Enable GPU acceleration | `--use-gpu` |
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
```

Run one profile:

```bash
npm run test:latency:cpu-1080p30
npm run test:latency:cpu-1080p60
npm run test:latency:gpu-4k60
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
