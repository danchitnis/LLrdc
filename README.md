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

To see verbose debug logs, you can use the following flags:
- `--debug-ffmpeg`: Shows real-time ffmpeg frame rate and encoder reports.
- `--debug-x11`: Shows X11 keyboard warnings and XFCE session startup errors.
- `--debug`: Enables both ffmpeg and X11 debug logging.

### Network and WebRTC Configuration

By default, the server auto-detects your primary IP address for WebRTC. If you have multiple network interfaces (e.g., Tailscale, VPNs, or multiple LANs), you can use the following flags to control which interfaces are used for the stream:

- `--iface <name>` or `-i <name>`: Only allow WebRTC to use the specified interface (e.g., `eth0`).
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

## Configuration Options

LLrdc can be configured using command-line flags (when running the binary directly in a custom container) or environment variables (when using `docker-run.sh`).

### Command-Line Flags

The `llrdc` binary supports the following flags, categorized by their primary use case:

#### User Flags
- `--port`: Port for both HTTP and WebRTC UDP (default: `8080`).
- `--fps`: Target frames per second (default: `30`).
- `--video-codec`: Choice of `vp8` (default), `h264`, `h264_nvenc`, `av1`, or `av1_nvenc`.
- `--use-gpu`: Enable GPU acceleration for NVENC codecs.
- `--use-debug-ffmpeg`: Enable verbose FFmpeg logging.
- `--use-debug-x11`: Enable verbose X11/XFCE session logging.
- `--display-num`: X11 display number inside the container (default: `99`).
- `--wallpaper`: Path to a custom wallpaper image.
- `--webrtc-public-ip`: Manually set the public IP for ICE candidates.
- `--webrtc-interfaces`: Comma-separated allowlist of network interfaces.
- `--webrtc-exclude-interfaces`: Comma-separated blocklist of network interfaces.

#### Testing Flags
- `--test-pattern`: Run with an FFmpeg `testsrc` pattern instead of capturing the X11 desktop.
- `--test-minimal-x11`: Start a bare X11 session without the full XFCE desktop environment (useful for automated UI tests).

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
| `USE_GPU` | Enable GPU acceleration | `--use-gpu` |
| `USE_DEBUG_FFMPEG` | Enable FFmpeg debug logs | `--use-debug-ffmpeg` |
| `USE_DEBUG_X11` | Enable X11 debug logs | `--use-debug-x11` |
| `WEBRTC_PUBLIC_IP` | Public IP override | `--webrtc-public-ip` |
| `TEST_PATTERN` | Use FFmpeg test pattern | `--test-pattern` |
| `TEST_MINIMAL_X11` | Skip XFCE startup | `--test-minimal-x11` |
| `WALLPAPER` | Custom wallpaper path | `--wallpaper` |
