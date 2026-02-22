# LLrdc

LLrdc (Low Latency remote desktop) is an entirely web-based, low-latency remote desktop and container solution.

## Features

- **XFCE4 Desktop in Docker**: Runs a full Ubuntu 24.04 and XFCE4 desktop environment inside a reproducible Docker container.
- **Web-Based Client**: Access your desktop entirely via a modern web browser—no client software required.
- **High-Performance Streaming**: Leverages WebRTC for ultra-low latency video streaming, with fallback to WebCodecs/WebSockets.



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

This will run the desktop container mapping port `8080` internally to port `8080` on the host. The script will automatically detect the number of host CPUs and explicitly map them to the container.

### 3. Connect

Open your browser and navigate to:

```
http://localhost:8080
```

You should see your XFCE4 desktop session running and ready for interaction.

## Configuration Options

You can pass environment variables to `docker-run.sh` to override the defaults:

```bash
PORT=9090 HOST_PORT=9090 FPS=60 VIDEO_CODEC=vp8 ./docker-run.sh
```
