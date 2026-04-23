# Native Low-Latency Optimization Guide

This document outlines the WebRTC network layer optimizations implemented to achieve ultra-low latency (ULL) for the LLrdc native client. These optimizations target the transport and queuing stages, specifically reducing the `Brd->Rec` (Broadcast to Receive) latency.

## Core Optimizations

1.  **Zero-Buffering (Direct Write)**: Bypasses the internal Go channel and goroutine on the server. Frames are written synchronously to the WebRTC track the moment they are captured.
2.  **Interceptor Bypass**: Disables default Pion interceptors (NACK generator, Jitter Buffer, RTCP reports) on both server and client. This removes the "wait-and-retransmit" logic which is harmful for real-time streaming on stable networks.
3.  **ICE/Transport Tuning**: Disables mDNS gathering and enforces UDP4 candidate selection to ensure the fastest possible connection path.
4.  **Replay Protection Disabled**: Disables SRTP/SRTCP replay protection overhead.

## How to Run

### 1. Automated Benchmark
To reproduce the latency breakdown and verify the improvements:

```bash
WEBRTC_LOW_LATENCY=true WEBRTC_BUFFER_SIZE=0 ./scripts/benchmark-native-latency.sh
```

The native benchmark reports `Control API -> Native Present` latency, not physical host input-to-photon latency. It uses a monotonic clock across the server, probe app, and native client, and it correlates each presented frame to one exact probe marker encoded into the frame.

### 2. Manual Execution (Server + Native Client)

**Start the Server (Docker):**
```bash
./docker-run.sh --webrtc-low-latency --webrtc-buffer 0
```

**Start the Native Client:**
```bash
npm run client:run -- --server http://127.0.0.1:8080 --auto-start
```

## Latency Statistics (1080p60 VP8)

Measured on April 23, 2026 with:

```bash
WEBRTC_LOW_LATENCY=true WEBRTC_BUFFER_SIZE=0 LLRDC_WARMUP_COUNT=3 LLRDC_SAMPLE_COUNT=5 ./scripts/benchmark-native-latency.sh
```

This run produced the following `Control API -> Native Present` stage averages:

| Stage | Average | Range |
| :--- | :--- | :--- |
| **Ctrl->Req** | `0.0 ms` | `0-0 ms` |
| **Render** | `18.8 ms` | `12-32 ms` |
| **Encode** | `6.6 ms` | `5-8 ms` |
| **Transit** | `52.4 ms` | `32-67 ms` |
| **Client** | `15.0 ms` | `10-24 ms` |
| **Overall E2E** | `92.8 ms` | `71-113 ms` |

Per-sample totals from that run:

| Marker | Total E2E |
| :--- | :--- |
| `4` | `84 ms` |
| `5` | `91 ms` |
| `6` | `98 ms` |
| `7` | `87 ms` |
| `8` | `95 ms` |

Notes:
- These numbers are from the native benchmark harness, not the browser Playwright lane.
- `Ctrl->Req` is millisecond-quantized in the current report, so sub-millisecond differences will show as `0 ms`.
- The largest contributor in this run was still `Transit`, even with low-latency WebRTC enabled.

## Configuration Flags

| Flag | Description |
| :--- | :--- |
| `WEBRTC_LOW_LATENCY=true` | Enables the interceptor bypass, ICE tuning, and replay protection disablement. |
| `WEBRTC_BUFFER_SIZE=0` | Enables synchronous "Direct Write" mode on the server (requires `WEBRTC_LOW_LATENCY=true`). |

---
*Note: These optimizations assume a stable local or high-quality network (e.g., LAN, Tailscale). On high-loss networks, disabling NACK may cause visible artifacts, as the system will rely entirely on PLI-triggered keyframes to recover from loss.*
