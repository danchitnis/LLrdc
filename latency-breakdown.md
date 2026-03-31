# Latency Benchmark Profiles

This file is the canonical benchmark note for LLrdc latency work. Old mixed baselines have been removed. Going forward, latency work should use only these two scenarios:

1. `low-end CPU`: `vp8`, `1080p`, `compat`, at `30 FPS` and `60 FPS`
2. `high-end GPU`: `av1_nvenc`, `4K`, `compat` vs `direct`, at `60 FPS`

## Reproducible Commands

Build once:

```bash
./docker-build.sh
```

Run one profile:

```bash
npm run test:latency:cpu-1080p30
npm run test:latency:cpu-1080p60
npm run test:latency:gpu-4k60
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

- `inputToRequest`: `3.59 ms`
- `requestToDraw`: `0.70 ms`
- `drawToFirstFrameBroadcast`: `18.85 ms`
- `firstFrameBroadcastToReceive`: `61.78 ms`
- `receiveToDecodeReady`: `2.72 ms`
- `decodeReadyToCompose`: `91.99 ms`
- `composeToExpectedDisplay`: `16.47 ms`
- `expectedDisplayToCallback`: `-0.07 ms`
- `drawToCallback`: `191.74 ms`
- `inputToCallback`: `196.03 ms`

### Profile B: Low-End CPU, VP8, 1080p60

Command:

```bash
npm run test:latency:cpu-1080p60
```

Observed stream:

- codec: `vp8`
- mode: `compat`
- resolution: about `1824x1072`
- status FPS: `59`

Average breakdown:

- `inputToRequest`: `4.30 ms`
- `requestToDraw`: `0.58 ms`
- `drawToFirstFrameBroadcast`: `12.06 ms`
- `firstFrameBroadcastToReceive`: `78.94 ms`
- `receiveToDecodeReady`: `2.71 ms`
- `decodeReadyToCompose`: `82.89 ms`
- `composeToExpectedDisplay`: `16.50 ms`
- `expectedDisplayToCallback`: `-7.16 ms`
- `drawToCallback`: `185.95 ms`
- `inputToCallback`: `190.82 ms`

Delta vs `1080p30`:

- `drawToCallback`: `-5.79 ms`
- `inputToCallback`: `-5.21 ms`
- `drawToFirstFrameBroadcast`: `-6.79 ms`
- `firstFrameBroadcastToReceive`: `+17.16 ms`
- `decodeReadyToCompose`: `-9.10 ms`

Takeaway:

- `60 FPS` helps the sender-side cadence and browser queue enough to slightly reduce end-to-end latency here.
- The biggest bucket is still `decodeReadyToCompose`, followed by `firstFrameBroadcastToReceive`.

### Profile C: High-End GPU, AV1 NVENC, 4K60

Command:

```bash
npm run test:latency:gpu-4k60
```

Observed stream:

- codec: `av1_nvenc`
- modes: `compat` and `direct`
- resolution: about `3808x2160`
- target bandwidth: `10 Mbps`
- status FPS: `58` in `compat`, `62` in `direct`

#### Compat

- `inputToRequest`: `37.03 ms`
- `requestToDraw`: `2.59 ms`
- `drawToFirstFrameBroadcast`: `6.57 ms`
- `firstFrameBroadcastToReceive`: `98.97 ms`
- `receiveToDecodeReady`: `5.63 ms`
- `decodeReadyToCompose`: `142.70 ms`
- `composeToExpectedDisplay`: `12.52 ms`
- `expectedDisplayToCallback`: `31.52 ms`
- `drawToCallback`: `297.91 ms`
- `inputToCallback`: `337.53 ms`

#### Direct

- `inputToRequest`: `26.13 ms`
- `requestToDraw`: `0.49 ms`
- `drawToFirstFrameBroadcast`: `6.98 ms`
- `firstFrameBroadcastToReceive`: `184.10 ms`
- `receiveToDecodeReady`: `6.03 ms`
- `decodeReadyToCompose`: `194.81 ms`
- `composeToExpectedDisplay`: `9.84 ms`
- `expectedDisplayToCallback`: `39.03 ms`
- `drawToCallback`: `440.79 ms`
- `inputToCallback`: `467.41 ms`

#### Delta (`direct - compat`)

- `inputToRequest`: `-10.90 ms`
- `requestToDraw`: `-2.10 ms`
- `drawToFirstFrameBroadcast`: `+0.41 ms`
- `firstFrameBroadcastToReceive`: `+85.13 ms`
- `receiveToDecodeReady`: `+0.40 ms`
- `decodeReadyToCompose`: `+52.11 ms`
- `composeToExpectedDisplay`: `-2.68 ms`
- `expectedDisplayToCallback`: `+7.51 ms`
- `drawToCallback`: `+142.88 ms`
- `inputToCallback`: `+129.88 ms`

Takeaway:

- In this exact `4K60 + av1_nvenc + 10 Mbps` profile, `direct` is significantly worse than `compat`.
- The loss is dominated by:
  - `firstFrameBroadcastToReceive`
  - `decodeReadyToCompose`
- So the current direct path is not ready to be the recommended low-latency path for the high-end AV1 profile.

## Current Conclusions

1. For the CPU profile, the dominant latency is still receiver-side buffering/presentation, not app draw.
2. `1080p60` VP8 is slightly better than `1080p30` VP8 in this environment.
3. For the GPU profile, `compat` currently beats `direct` at `4K60` with `av1_nvenc`.
4. The biggest optimization target in all profiles remains the receive/presentation side, especially:
   - `firstFrameBroadcastToReceive`
   - `decodeReadyToCompose`

## Known Benchmark Caveats

1. The browser under test is Playwright Chromium, which is good for reproducible relative comparisons but weaker for absolute end-user latency claims than a normal headed Chrome session.
2. The `4K60` AV1 profile still has an occasional probe-sync flake; the benchmark now retries automatically to make repeated runs practical.
3. The “Video Latency” number shown in the UI is not the same thing as this event-to-photon breakdown and should not be used as the source of truth.
