# Development and Testing Guide: Wayland Native Client

This guide explains how to build, run, and verify the Wayland native client.

## Building the Client

The native client depends on several C libraries (`SDL2`, `libavcodec`, `libavutil`, `libswscale`). To ensure a consistent build environment, always use the provided Docker-based packaging script.

```bash
# Builds the client inside a container and exports the binary to ./dist
./scripts/package-native-client.sh
```

The resulting package will be located at `dist/llrdc-client-linux-amd64/`.

## Running the Client

### 1. Start the Server
Before running the client, you must have a server instance running. You can use the local Docker runner:

```bash
./docker-run.sh
```

### 2. Launch the Client
Run the client binary from the distribution folder. It is recommended to use `--auto-start` to bypass the initial "Click to Start" screen during testing.

```bash
# Run in windowed mode (requires a Wayland/X11 session)
./dist/llrdc-client-linux-amd64/bin/llrdc-client --server http://127.0.0.1:8080 --auto-start

# Run in headless mode (for CLI-only environments or CI)
./dist/llrdc-client-linux-amd64/bin/llrdc-client --server http://127.0.0.1:8080 --auto-start --headless
```

## Verifying the Connection (WebTransport)

The client now uses **WebTransport** (QUIC) as its primary transport. To verify that it is working correctly:

1.  **Check Logs:** Look for `WebTransport connected successfully`.
2.  **Monitor Stats:** The client exposes a Control API on port `18080`. You can query it to ensure frames are being presented:

```bash
# Query the stats endpoint
curl -s http://127.0.0.1:18080/statsz | jq .
```

**Success Criteria:**
- `webtransportConnected` (in `/readyz`) must be `true`.
- `presentedFrames` (in `/statsz`) should be incrementing steadily.
- `decodeErrors` should remain at or near `0`.

## Automated Benchmarking

See the canonical repository test catalog: [test.md](../../test.md).

## Troubleshooting

- **Handshake Failures:** If you see `websocket: bad handshake`, ensure the server is fully ready by checking `http://127.0.0.1:8080/readyz`.
- **Decoding Errors:** If you see `FFmpeg decode error`, check if the `videoCodec` reported in `statsz` matches the stream being sent.
- **No Windows:** Ensure your `WAYLAND_DISPLAY` or `DISPLAY` environment variables are correctly set if running in windowed mode.
