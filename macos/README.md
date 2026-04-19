# LLrdc macOS Native Client

This directory contains the native macOS client for LLrdc. Unlike the Linux client, this version is written using Pure Go + Cgo and leverages Apple's native frameworks (**AppKit**, **AVFoundation**, and **VideoToolbox**) for windowing and hardware-accelerated video decoding.

## Features
- **Zero Global Dependencies**: No need to install `sdl2` or `libvpx` via Homebrew.
- **Hardware Acceleration**: Uses Apple's VideoToolbox for low-latency H.264 decoding.
- **Native Experience**: Packaged as a standard `.app` bundle with support for high-resolution displays.

## Requirements
- macOS 14.0 or later.
- Go 1.21 or later.
- Xcode Command Line Tools (`xcode-select --install`).

## Building
Run the provided build script from the project root:

```bash
./macos/build.sh
```

This will create `macos/LLrdc.app`.

## Running
The macOS native client uses hardware H.264 decoding. You **must** ensure the LLrdc server is configured to stream H.264.

1. Start the server with the H.264 codec:
   ```bash
   go run cmd/server/*.go -video-codec h264
   ```

2. Launch the client:
   ```bash
   open macos/LLrdc.app --args -server http://<SERVER_IP>:8080
   ```
   The macOS client now auto-starts streaming by default. To keep the click-to-start overlay, add `--auto-start=false`.

## Controls
- **Mouse**: Moving the mouse sends cursor position; left/right clicks and scroll wheel are supported.
- **Keyboard**: Standard alphanumeric keys are mapped to the remote session.
- **Window**: Resizing the native window will request a resolution change from the server.
