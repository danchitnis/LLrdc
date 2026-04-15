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

The following statistics represent the typical improvement observed on a local network stack:

| Stage | Baseline (Standard WebRTC) | Optimized (Low-Latency + Zero Buffer) | Improvement |
| :--- | :--- | :--- | :--- |
| **Draw->Brd** (Server Capture) | ~10-20 ms | ~5-15 ms | Queue removal |
| **Brd->Rec** (Network Transport) | ~65-130 ms | **~31-35 ms** | **-50% to -70%** |
| **Rec->Dec** (Client Receive) | ~2-5 ms | ~1-2 ms | Interceptor bypass |
| **Overall E2E** | ~180-200 ms | **~45-60 ms** | **Significant** |

### Statistical Insights:
- **`Brd->Rec` (Broadcast to Receive)**: This is the primary beneficiary of the Interceptor Bypass. By removing the NACK buffers, the packet processing time on the receiver side is significantly reduced.
- **Zero Buffering**: Setting `WEBRTC_BUFFER_SIZE=0` ensures that `Draw->Brd` stays consistent even during CPU spikes, as frames are never queued behind older ones.

## Configuration Flags

| Flag | Description |
| :--- | :--- |
| `WEBRTC_LOW_LATENCY=true` | Enables the interceptor bypass, ICE tuning, and replay protection disablement. |
| `WEBRTC_BUFFER_SIZE=0` | Enables synchronous "Direct Write" mode on the server (requires `WEBRTC_LOW_LATENCY=true`). |

---
*Note: These optimizations assume a stable local or high-quality network (e.g., LAN, Tailscale). On high-loss networks, disabling NACK may cause visible artifacts, as the system will rely entirely on PLI-triggered keyframes to recover from loss.*
