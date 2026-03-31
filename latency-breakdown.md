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

Date of these runs: `2026-03-31`

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

- `inputToRequest`: `3.34 ms`
- `requestToDraw`: `0.55 ms`
- `drawToFirstFrameBroadcast`: `19.18 ms`
- `firstFrameBroadcastToReceive`: `103.72 ms`
- `receiveToDecodeReady`: `2.42 ms`
- `decodeReadyToCompose`: `27.28 ms`  *(Massive improvement from previous ~92 ms)*
- `composeToExpectedDisplay`: `16.54 ms`
- `expectedDisplayToCallback`: `2.76 ms`
- `drawToCallback`: `171.90 ms`
- `inputToCallback`: `175.79 ms`

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

- `inputToRequest`: `5.69 ms`
- `requestToDraw`: `0.58 ms`
- `drawToFirstFrameBroadcast`: `7.02 ms`
- `firstFrameBroadcastToReceive`: `135.89 ms`
- `receiveToDecodeReady`: `2.72 ms`
- `decodeReadyToCompose`: `32.64 ms` *(Massive improvement from previous ~83 ms)*
- `composeToExpectedDisplay`: `8.04 ms`
- `expectedDisplayToCallback`: `0.63 ms`
- `drawToCallback`: `186.94 ms`
- `inputToCallback`: `193.21 ms`

Takeaway:

- `playoutDelayHint = 0` and drop-from-head buffering strategies have significantly reduced the `decodeReadyToCompose` bottleneck in the browser. 

### Profile C: High-End GPU, AV1 NVENC, 4K30

*(Note: Run at 30 FPS instead of 60 FPS due to headless chromium decoding limitations overwhelming the test harness)*

Command:

```bash
LLRDC_USE_GPU=true LLRDC_CAPTURE_MODES=compat,direct LLRDC_TARGET_VIDEO_CODEC=av1_nvenc LLRDC_TARGET_FPS=30 LLRDC_TARGET_MAX_RES=2160 LLRDC_TARGET_BANDWIDTH_MBPS=20 LLRDC_TARGET_VIEWPORT_WIDTH=3840 LLRDC_TARGET_VIEWPORT_HEIGHT=2220 npx playwright test tests/wayland_latency_breakdown.spec.ts
```

Observed stream:

- codec: `av1_nvenc`
- modes: `compat` and `direct`
- resolution: about `3808x2160`
- target bandwidth: `20 Mbps`
- status FPS: `30` in `compat`, `30` in `direct`

#### Compat

- `inputToRequest`: `7.17 ms`
- `requestToDraw`: `2.52 ms`
- `drawToFirstFrameBroadcast`: `17.21 ms`
- `firstFrameBroadcastToReceive`: `62.46 ms`
- `receiveToDecodeReady`: `5.50 ms`
- `decodeReadyToCompose`: `76.02 ms`
- `composeToExpectedDisplay`: `16.05 ms`
- `expectedDisplayToCallback`: `8.81 ms`
- `drawToCallback`: `186.05 ms`
- `inputToCallback`: `195.74 ms`

#### Direct

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

#### Delta (`direct - compat`)

- `inputToRequest`: `+0.63 ms`
- `requestToDraw`: `-2.20 ms`
- `drawToFirstFrameBroadcast`: `-0.42 ms`
- `firstFrameBroadcastToReceive`: `+8.93 ms`
- `receiveToDecodeReady`: `+0.00 ms`
- `decodeReadyToCompose`: `-0.17 ms`
- `composeToExpectedDisplay`: `-2.94 ms`
- `expectedDisplayToCallback`: `+3.85 ms`
- `drawToCallback`: `+9.25 ms`
- `inputToCallback`: `+7.68 ms`

Takeaway:

- Recent optimizations to `direct` mode have drastically closed the gap between it and `compat` mode (down from a ~130ms deficit).
- `direct` mode successfully bypasses the compositor layer, reducing `requestToDraw` by >2ms consistently.
- Overall end-to-end latency is now virtually tied (~8ms difference), meaning `direct` mode is a highly viable path for future AV1 ULL configurations.

## Current Conclusions

1. `playoutDelayHint = 0` effectively resolved the massive receiver-side buffering delay (`decodeReadyToCompose` dropped from ~90ms to ~30ms at 1080p).
2. For the GPU profile, `direct` is now within striking distance of `compat` (only ~8ms behind overall at 4K), resolving previous severe regressions in the direct capture path.
3. The biggest optimization target is now shifting slightly towards network/transport (`firstFrameBroadcastToReceive`) and `decodeReadyToCompose` at higher resolutions (4K).

## Known Benchmark Caveats

1. The browser under test is Playwright Chromium, which is good for reproducible relative comparisons but weaker for absolute end-user latency claims than a normal headed Chrome session.
2. The `4K60` profile consistently fails under Playwright due to headless browser 4K60 decoding performance (even on an RTX 4090). Benchmarks for 4K must currently be executed at 30FPS for stability.
3. The “Video Latency” number shown in the UI is not the same thing as this event-to-photon breakdown and should not be used as the source of truth.
