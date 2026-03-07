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

```bash
./docker-run.sh --debug
```

### 3. Connect

Open your browser and navigate to:

```
http://localhost:8080
```

You should see your XFCE4 desktop session running and ready for interaction.

## Configuration Options

You can pass environment variables to `docker-run.sh` to override the defaults:

```bash
PORT=9090 HOST_PORT=9090 FPS=60 VIDEO_CODEC=h264 ./docker-run.sh
```

Available environment variables:
- `PORT`: Server internal port (default: 8080)
- `HOST_PORT`: Port exposed to the host (default: 8080)
- `FPS`: Target frames per second (default: 30)
- `VIDEO_CODEC`: `vp8` (default), `h264`, or `h264_nvenc`
- `DISPLAY_NUM`: X11 display number (default: 99)
- `USE_DEBUG_FFMPEG`: Set to `true` to enable ffmpeg debug logging (equivalent to `--debug-ffmpeg` flag)
- `USE_DEBUG_X11`: Set to `true` to enable X11 debug logging (equivalent to `--debug-x11` flag)
- `USE_GPU`: Set to `true` to enable GPU acceleration (equivalent to `--gpu` flag)
