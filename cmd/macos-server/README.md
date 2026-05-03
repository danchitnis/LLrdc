# macOS Split Architecture (Hardware Encoding)

This architecture allows LLrdc to utilize Apple's native GPU for high-performance, low-latency video encoding (via VideoToolbox) while keeping the Linux desktop environment (Wayland/XFCE) completely isolated within Docker.

## How it works

The system is split across the virtualization boundary into two components:

1. **Docker Agent (`Dockerfile.macos`)**: Runs headless Labwc and the XFCE desktop. It uses `wf-recorder` to capture uncompressed raw YUV420p video frames and streams them instantly over a local TCP loopback (`host.docker.internal:12345`). It also listens on a TCP port (`12346`) to receive instant input commands (mouse/keyboard) from the host.
2. **macOS Native Server (`cmd/macos-server`)**: Runs natively on your Mac. It hosts the WebRTC/HTTP stack, manages browser connections, and uses Apple's **VideoToolbox (CGO)** to encode the incoming raw frames to a low-latency H.264 bitstream. It also routes your browser's input data back over TCP to the Docker container in real-time.

## Prerequisites

- macOS (Apple Silicon or Intel).
- Docker Desktop (or OrbStack).
- Go 1.24+.
- Node.js (for building the Web UI).
- Xcode Command Line Tools (`xcode-select --install` required for CGO compilation).

---

## 1. Build the System

First, build the frontend Web UI:
```bash
npm install
npm run build
```

Next, build the specialized Docker agent image:
```bash
./docker-build.sh --macos
```

Finally, compile the native macOS Go server:
```bash
go build -o macos-server ./cmd/macos-server
```

---

## 2. Run the System

You must start both components to establish the bridge.

**Terminal 1 (Host Server):**
Start the macOS native server. It will wait for the Docker agent to connect.
```bash
./macos-server
```
*(Note: You can pass `--fps 60` or `--port 8080` to modify the default settings).*

**Terminal 2 (Docker Agent):**
Start the Docker container using the provided run script. It maps the necessary ports and connects to the host's Video Receiver.
```bash
./docker-run-macos.sh
```

---

## 3. Connect

Open your browser and navigate to:
```
http://localhost:8080/viewer.html
```

You should now see the XFCE desktop streaming in H.264 at 60 FPS, with instantaneous input responsiveness and minimal CPU usage.

## Architecture Notes
- The Go server in Docker (`llrdc`) is run with `--capture-mode agent` and `--fps 60`, which bypasses local encoding and pipes raw frames out via `wf-recorder`.
- To prevent network bloat, the macOS `video_receiver.go` uses a 1-frame deep `sync.Pool` dropping queue. If the hardware encoder falls behind, intermediate frames are silently dropped to guarantee absolute zero-latency for the freshest frame.
- Input commands bypass Wayland queue aggregation and are written directly to `wayland_input_client.c` via standard input for immediate execution.
- In WebRTC mode, the frontend `input.ts` binds to the active `<video>` element, ensuring mouse coordinates scale perfectly regardless of window size.