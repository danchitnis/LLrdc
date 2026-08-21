# LLrdc

LLrdc (Low Latency remote desktop) is an entirely web-based, low-latency remote desktop and container solution.

## Features

- **XFCE4 Desktop in Docker**: Runs a full Ubuntu 24.04 and XFCE4 desktop environment inside a reproducible Docker container.
- **Web-Based Client**: Access your desktop entirely via a modern web browser—no client software required.
- **High-Performance Streaming**: Uses WebTransport over HTTP/3 for low-latency video and control, with a WebSocket fallback for browsers without WebTransport support. Uses variable bitrate (bitrate drops on static screens) with an optional peak bandwidth cap.
- **Native Go Client**: Includes a Docker-built Linux native client using Go, SDL2, Wayland presentation feedback, and FFmpeg decode libraries.

The supported surfaces are the Linux Docker server (CPU, Intel, or NVIDIA), the macOS split server, the browser client, and the native Linux/macOS clients. Linux `agent` capture mode is an internal transport used by the macOS split server.



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

If you want Intel VAAPI support, build the Intel variant explicitly:

```bash
./docker-build.sh --intel
```

This creates `danchitnis/llrdc:intel`, which includes the Intel media drivers, VAAPI tooling, and related FFmpeg acceleration stack.

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

To enable Intel VAAPI acceleration, build the Intel image first and then run with `--intel`:

```bash
./docker-build.sh --intel
./docker-run.sh --intel
```

When `--intel` is passed, `docker-run.sh` automatically targets the `:intel` image tag unless `IMAGE_TAG` is explicitly set. If you force `IMAGE_TAG=latest`, the script will fail fast because `:latest` is now the CPU-only image.

To request the new GPU direct-buffer path, use `--capture-mode direct` together with `--nvidia` or `--intel`:

```bash
# NVIDIA example
./docker-run.sh --nvidia --capture-mode direct

# Intel H.265 4:4:4 High-Performance example
./docker-run.sh --intel --capture-mode direct --video-codec hevc_vaapi --chroma 444
```

This mode is fail-closed: startup aborts unless the compositor exposes the required Wayland screencopy and linux-dmabuf capabilities.

To see verbose debug logs, you can use the following flags:
- `--debug-ffmpeg`: Shows real-time ffmpeg frame rate and encoder reports.
- `--debug`: Enables both ffmpeg and input debug logging.
- `--hdpi [percent]` or `-h [percent]`: Enables High DPI scaling for the XFCE desktop. If no percentage is provided, it defaults to `200` (2x scaling). Example: `--hdpi 150` for 1.5x scaling.
- `--capture-mode compat|direct|agent`: Selects the capture path. `direct` requires `--nvidia` or `--intel`; `agent` is reserved for the macOS split server.
- `--activity-hz [hz]`: Sets the input activity heartbeat frequency (default: `30`).
- `--activity-timeout [ms]`: Sets how long the heartbeat continues after last input (default: `1500`).
- `--no-nvenc-latency`: Disables ultra-low-latency NVENC optimizations.

### Network and transport configuration

The HTTP/WebSocket server listens on `PORT` (default `8080`). WebTransport uses `PORT + 10` over both TCP and UDP (default `8090`). `docker-run.sh` publishes both ports automatically when bridge networking is used; `--host-net` uses the host network directly.

### 3. Connect

Open your browser and navigate to:

```
http://localhost:8080
```

You should see your XFCE4 desktop session running and ready for interaction.

## macOS Split Architecture

LLrdc features a specialized "Split" architecture for macOS users. This mode allows you to run the heavy desktop environment and Wayland session inside a Docker container (as usual), while utilizing a **native macOS host server** for high-performance H.264 encoding via Apple's **VideoToolbox** framework.

### Why Use Split Mode?

- **Hardware Acceleration**: Access native macOS hardware encoding (VideoToolbox) which is not typically accessible from within a standard Docker container.
- **Ultra-Low Latency**: Benefit from Apple's optimized silicon for video encoding, providing a smoother experience on Mac hardware.
- **Seamless Integration**: The browser connects to the native macOS host, which orchestrates the Docker container in the background.

### How to Run (macOS only)

To build and launch the split architecture environment, use the provided helper script:

```bash
./run-macos-split.sh
```

This script will:
1. Build the frontend and the native macOS host server.
2. Build/verify the specialized Docker agent container.
3. Launch the host server and the Docker agent in the background.
4. Provide a URL to access the remote desktop (`http://localhost:8080/viewer.html`).

To stop the session and clean up all background processes and containers, simply press `Ctrl+C` in the terminal where the script is running.

## Native Client

The repo also includes a native Linux client in [cmd/client/main.go](/home/danial/code/LLrdc/cmd/client/main.go). It is built and tested inside Docker from [Dockerfile.client](/home/danial/code/LLrdc/Dockerfile.client), but runs as a real SDL windowed client rather than embedding a browser.

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
- Native Linux client runtime is Wayland-only.
- The host must provide `XDG_RUNTIME_DIR` and a `WAYLAND_DISPLAY` socket.
- `WAYLAND_DISPLAY` defaults to `wayland-0` when unset.
- The authoritative Linux latency lane is the native SDL/Wayland client. The separate browser lane uses the installed Google Chrome binary on the active Wayland session for functional checks only.

### Verify the Packaged Host Runtime

This runs the packaged client directly on the host in `--headless` mode to verify the exported binary and bundled libraries work outside Docker:

```bash
npm run client:verify-package
```

### Test the Native Client

The native Linux client test uses Dockerized native/cgo unit tests plus the latency benchmark. This is the authoritative Linux native lane because it packages the client, launches a dedicated Weston bench, connects to a real LLrdc server over same-host WebTransport, drives deterministic input, decodes the visual sample ID, and verifies destination Wayland presentation feedback.

```bash
npm run client:test
```

That command runs host-safe Go tests, builds the native/cgo client test image, and executes the authoritative benchmark.

Run the benchmark directly when you want to tune sample count, codec, or ULL settings:

```bash
npm run client:benchmark:latency
```

- `client:benchmark:latency` packages the client, launches a dedicated Weston bench, drives deterministic pointer moves through the native client control API, and reports stage timings from control injection through native present using a monotonic clock plus probe-marker correlation.

The destination Wayland surface must be foreground and visible to its compositor. The benchmark checks focus and requires at least one `wp_presentation` **presented** callback before sampling; an occluded or superseded surface is a hard failure. An installed desktop is not sufficient by itself: an SSH shell does not carry GNOME's application-activation context, so a nested Weston window started remotely can remain unfocused and correctly produce only discarded feedback.

For `nzxt5`, synchronize the repository and run the remote-session wrapper from SSH:

```bash
ssh nzxt5 'cd ~/code/LLrdc && ./scripts/run-remote-wayland-latency.sh'
```

By default the wrapper starts an isolated headless `labwc` Wayland destination in a disposable NVIDIA-image container. This avoids SSH/GNOME focus and lock-state problems while still requiring real `wp_presentation` feedback. It imports the active user environment when available, synchronizes the source and destination presentation clock, and fails instead of substituting SDL render-submit time. To measure against the physical GNOME compositor instead, use `LLRDC_DESTINATION_COMPOSITOR=gnome`; that requires an unlocked local or GNOME Remote Desktop/RDP session and launches the native client through GNOME application activation. Do not use `ssh -X` or an X11-only session for this benchmark.

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
- `--port`: HTTP/WebSocket server port (default: `8080`). WebTransport uses `PORT + 10`.
- `--fps`: Target frames per second (default: `30`).
- `--video-codec`: Choice of `vp8` (default), `h264`, `h264_nvenc`, `h264_vaapi`, `h265`, `h265_nvenc`, `h265_vaapi`, `hevc_vaapi`, `av1`, `av1_nvenc`, or `av1_vaapi`.
- `--chroma`: Chroma subsampling format, `420` (default) or `444`. See [Chroma 4:4:4](#chroma-444) below.
- `--use-nvidia`: Enable NVIDIA acceleration for NVENC codecs.
- `--use-intel`: Enable Intel acceleration for VAAPI codecs.
- `--capture-mode`: Capture mode, `compat` (default), `direct`, or internal `agent`.
- `--use-debug-ffmpeg`: Enable verbose FFmpeg logging.
- `--use-debug-input`: Enable verbose input logging.
- `--wallpaper`: Path to a custom wallpaper image.
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
| `USE_DEBUG_INPUT` | Enable input debug logs | `--use-debug-input` |
| `ACTIVITY_PULSE_HZ` | Heartbeat frequency (Hz) | `--activity-hz` |
| `ACTIVITY_TIMEOUT` | Inactivity timeout (ms) | `--activity-timeout` |
| `NVENC_LATENCY_MODE` | Toggle NVENC ULL (Ultra Low Latency) | `--nvenc-latency` |
| `TEST_PATTERN` | Use FFmpeg test pattern | `--test-pattern` |
| `WALLPAPER` | Custom wallpaper path | `--wallpaper` |
| `ENABLE_CLIPBOARD` | Enable clipboard sync | `--enable-clipboard` |

## Reproducible Benchmarks

The authoritative benchmark is the native Wayland client lane. It runs H.264 at 1920×1080/60 with VBR disabled, uses same-host WebTransport, and ends at destination Wayland presentation feedback. Each accelerator invocation reports one capture mode independently; GPU lanes default to `direct`, while CPU uses `compat`.

```bash
./docker-build.sh --nvidia       # NVIDIA surface
# ./docker-build.sh --intel     # Intel surface
# ./docker-build.sh              # CPU surface
```

Run the NVIDIA direct-path benchmark:

```bash
npm run test:latency:nvidia
```

Run the CPU or Intel lanes with the same measurement and report format:

```bash
npm run test:latency:cpu
npm run test:latency:intel
```

On `nzxt5`, use the generic host-local runner:

```bash
./scripts/benchmark-latency.sh
```

This defaults to the NVIDIA lane. Select another surface explicitly:

```bash
./scripts/benchmark-latency.sh --accel cpu
./scripts/benchmark-latency.sh --accel intel
```

Select `cpu`, `intel`, or `nvidia` with `--accel`. The runner chooses
H.264 software, VAAPI, or NVENC respectively, runs the supported capture
comparison, and writes results to a timestamped directory under
`.artefact/<accelerator>/`. Use `--mode direct --samples 20` for a quick
GPU direct-path check.

Each invocation reports its own accelerator, codec, capture mode, percentile statistics, and stage breakdown. Intel and NVIDIA can be run independently in `compat` mode with `--mode compat`; CPU supports `compat` only.

Run the separate installed-Chrome functional lane:

```bash
npm run test:browser:chrome:nvidia
```

The native report records nanosecond stages for control delivery, input injection, source commit and presentation, capture/encode, WebTransport write, client read/decode, SDL submission, and destination compositor presentation. It rejects duplicate/undecodable/discarded/non-monotonic samples and never repairs timestamps. Its endpoint is compositor feedback, not physical photon emission; a camera or photodiode is required for that.

Notes:
- The benchmark disables VBR for reproducibility.
- On the Intel Linux surface, direct capture uses the `h264_vaapi` encoder in
  the DMA-BUF `wf-recorder` path; reports include the requested codec and the
  verified encoder backend.
- Chrome canvas/WebCodecs callbacks are not used for authoritative latency measurements.

## Chroma 4:4:4

Chroma 4:4:4 avoids chroma subsampling, improving clarity for text and sharp edges on remote desktops. It can be toggled at runtime from the config panel (Quality tab) or set at startup with `--chroma 444`.

### Codec Support (Chroma 4:4:4)

| Codec | 4:4:4 Support | Notes |
| :--- | :--- | :--- |
| `h264_vaapi` (Intel) | ❌ | Restricted to 4:2:0. **Low CPU usage** via hardware acceleration. |
| `h265_vaapi` (Intel) | ✅ | **4:4:4 support**. Low CPU usage via hardware-accelerated conversion. |
| `h264/h265` (NVIDIA) | ✅ | **4:4:4 support** on compatible GPUs. |
| `CPU codecs` | ❌ | Restricted to 4:2:0. High CPU usage. |

> **Note:** Chroma 4:4:4 is supported on the **H.265 (HEVC) Intel** hardware path, as well as **H.264 and H.265 NVIDIA NVENC** paths (on supported hardware). All other configurations, including H.264 Intel, are limited to 4:2:0. Both Intel and NVIDIA GPU paths feature low CPU overhead due to hardware-accelerated chroma conversion and encoding.
