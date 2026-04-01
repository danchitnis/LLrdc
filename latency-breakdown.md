# Latency Benchmark Profiles

This file is the canonical benchmark note for LLrdc latency work. Old mixed baselines have been removed. Going forward, latency work should use only these two scenarios:

1. `low-end CPU`: `vp8`, `1080p`, `compat`, at `30 FPS` and `60 FPS`
2. `high-end GPU`: `av1_nvenc`, `4K`, `compat` vs `direct`, at `30 FPS`

## Reproducible Commands

Build once:

```bash
./docker-build.sh
```

Run one profile:

```bash
npm run test:latency:cpu-1080p30
npm run test:latency:cpu-1080p60
# Note: 4K60 tests currently suffer from headless browser decoding bottlenecks. 
# 4K testing is done at 30fps using LLRDC_TARGET_FPS=30
```

Run the full matrix:

```bash
npm run test:latency:profiles
```

Implementation:

- benchmark harness: [tests/wayland_latency_breakdown.spec.ts](/home/danial/code/LLrdc/tests/wayland_latency_breakdown.spec.ts)
- batch runner: [scripts/run-latency-profiles.sh](/home/danial/code/LLrdc/scripts/run-latency-profiles.sh)
- server trace hook: [cmd/server/latency_probe.go](/home/danial/code/LLrdc/cmd/server/latency_probe.go)
- HTTP endpoint: [cmd/server/http.go](/home/danial/code/LLrdc/cmd/server/http.go)
- remote probe app: [tools/latency_probe_app.py](/home/danial/code/LLrdc/tools/latency_probe_app.py)

## Measurement Model

The breakdown reports:

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

Interpretation:

- `requestToDraw` is remote app work.
- `drawToFirstFrameBroadcast` is server capture and first send visibility.
- `firstFrameBroadcastToReceive` is transport plus receiver availability.
- `receiveToDecodeReady` is decode work.
- `decodeReadyToCompose` is the browser/player queue before presentation.
- `drawToCallback` and `inputToCallback` are the main end-to-end totals.

## Latest Results

Date of these runs: `2026-03-31` (Post-WebRTC Transport Optimizations)

Summary of changes:
- Disabled SRTP/SRTCP replay protection in Pion `SettingEngine`.
- Enabled ICE Lite and disabled mDNS candidate generation.
- Verified that `firstFrameBroadcastToReceive` shows improvement at 60fps.

### Profile A: Low-End CPU, VP8, 1080p30

Command:

```bash
npm run test:latency:cpu-1080p30
```

Observed stream:

- codec: `vp8`
- mode: `compat`
- resolution: about `1824x1072`
- status FPS: `30`

Average breakdown:

- `inputToRequest`: `2.46 ms`
- `requestToDraw`: `0.60 ms`
- `drawToFirstFrameBroadcast`: `18.52 ms`
- `firstFrameBroadcastToReceive`: `104.40 ms`
- `receiveToDecodeReady`: `2.41 ms`
- `decodeReadyToCompose`: `29.95 ms`
- `composeToExpectedDisplay`: `16.52 ms`
- `expectedDisplayToCallback`: `1.02 ms`
- `drawToCallback`: `172.81 ms`
- `inputToCallback`: `175.88 ms`

### Profile B: Low-End CPU, VP8, 1080p60

Command:

```bash
npm run test:latency:cpu-1080p60
```

Observed stream:

- codec: `vp8`
- mode: `compat`
- resolution: about `1824x1072`
- status FPS: `60`

Average breakdown:

- `inputToRequest`: `9.65 ms`
- `requestToDraw`: `0.62 ms`
- `drawToFirstFrameBroadcast`: `9.41 ms`
- `firstFrameBroadcastToReceive`: `133.64 ms` (Improved from ~136ms)
- `receiveToDecodeReady`: `2.45 ms`
- `decodeReadyToCompose`: `29.55 ms`
- `composeToExpectedDisplay`: `8.34 ms`
- `expectedDisplayToCallback`: `-0.97 ms`
- `drawToCallback`: `182.42 ms`
- `inputToCallback`: `192.69 ms`

### Profile C: High-End GPU, AV1 NVENC, 4K30

Command:

```bash
LLRDC_USE_GPU=true LLRDC_CAPTURE_MODES=compat,direct LLRDC_TARGET_VIDEO_CODEC=av1_nvenc LLRDC_TARGET_FPS=30 LLRDC_TARGET_MAX_RES=2160 LLRDC_TARGET_BANDWIDTH_MBPS=20 LLRDC_TARGET_VIEWPORT_WIDTH=3840 LLRDC_TARGET_VIEWPORT_HEIGHT=2220 npx playwright test tests/wayland_latency_breakdown.spec.ts
```

Observed stream:

- codec: `av1_nvenc`
- modes: `compat` (tested), `direct` (harness stability issues)
- resolution: about `3808x2160`
- status FPS: `30`

#### Compat

- `inputToRequest`: `3.09 ms`
- `requestToDraw`: `2.18 ms`
- `drawToFirstFrameBroadcast`: `12.59 ms`
- `firstFrameBroadcastToReceive`: `72.53 ms`
- `receiveToDecodeReady`: `5.61 ms`
- `decodeReadyToCompose`: `102.46 ms`
- `composeToExpectedDisplay`: `16.55 ms`
- `expectedDisplayToCallback`: `13.60 ms`
- `drawToCallback`: `223.33 ms`
- `inputToCallback`: `228.60 ms`

#### Direct

*(Note: Direct mode currently experiencing timeout issues in the automated benchmark harness at 4K resolution; last successful baseline below for reference)*

- `inputToRequest`: `7.81 ms`
- `requestToDraw`: `0.31 ms`
- `drawToFirstFrameBroadcast`: `16.79 ms`
- `firstFrameBroadcastToReceive`: `71.39 ms`
- `receiveToDecodeReady`: `5.50 ms`
- `decodeReadyToCompose`: `75.85 ms`
- `composeToExpectedDisplay`: `13.11 ms`
- `expectedDisplayToCallback`: `12.66 ms`
- `drawToCallback`: `195.30 ms`
- `inputToCallback`: `203.42 ms`

## Current Conclusions

1. `playoutDelayHint = 0` effectively resolved the massive receiver-side buffering delay (`decodeReadyToCompose` dropped from ~90ms to ~30ms at 1080p).
2. WebRTC Transport Optimizations (disabling replay protection) provided a measurable **~2.2ms reduction** in `firstFrameBroadcastToReceive` at 1080p60.
3. High-resolution (4K) performance is currently dominated by `decodeReadyToCompose` (~100ms), indicating browser-side presentation queuing is the next major bottleneck for 4K ULL.
4. `direct` mode remains highly viable for reducing `requestToDraw` but requires further stability work in the automated test harness at 4K.

## Known Benchmark Caveats

1. The browser under test is Playwright Chromium, which is good for reproducible relative comparisons but weaker for absolute end-user latency claims than a normal headed Chrome session.
2. The `4K60` profile consistently fails under Playwright due to headless browser 4K60 decoding performance (even on an RTX 4090). Benchmarks for 4K must currently be executed at 30FPS for stability.
3. The “Video Latency” number shown in the UI is not the same thing as this event-to-photon breakdown and should not be used as the source of truth.
