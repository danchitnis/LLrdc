# Advice For NVIDIA Direct

## Current Situation

- The split image flow is in place:
  - default image: `danchitnis/llrdc:latest`
  - NVIDIA direct image: `danchitnis/llrdc:latest-nvidia-direct`
- Intel direct mode is still isolated on the existing `wf-recorder` + VAAPI/QSV path.
- NVIDIA direct mode is isolated behind the NVIDIA-only image and helper path.
- The current NVIDIA helper implementation is not a valid final solution.

## What To Stop Doing

- Do not keep iterating on `gpu-screen-recorder` for the repo's headless Wayland direct mode.
- Do not treat the current NVIDIA helper as "close enough" to zero-copy.
- Do not use image/backend identity alone as proof that NVIDIA direct is validated.

## What Was Tried

- A separate NVIDIA direct helper was added in [cmd/nvidia_direct_capture/main.go](/home/danial/code/LLrdc/cmd/nvidia_direct_capture/main.go) and [cmd/nvidia_direct_capture/pipeline.go](/home/danial/code/LLrdc/cmd/nvidia_direct_capture/pipeline.go).
- That helper was switched from `wf-recorder` to `gpu-screen-recorder` plus `ffmpeg -c copy` remux.
- The NVIDIA direct image builds successfully and includes `gpu-screen-recorder`.

## Why It Still Fails

- The app’s Wayland environment in [cmd/server/wayland.go](/home/danial/code/LLrdc/cmd/server/wayland.go) runs `labwc` with:
  - `WLR_BACKENDS=headless`
  - a synthetic `HEADLESS-1` output
- `gpu-screen-recorder` on Wayland monitor capture requires a DRM-backed monitor/card path.
- In this headless compositor setup, `gpu-screen-recorder` exits with:
  - `gsr error: no /dev/dri/cardX device found`
- Result:
  - [tests/nvidia/wayland_direct_buffer.spec.ts](/home/danial/code/LLrdc/tests/nvidia/wayland_direct_buffer.spec.ts) fails because no decodable stream is produced.
  - The low-CPU target is not verified and should be treated as unproven.

## Recommended Direction

- Replace the current `gpu-screen-recorder`-based NVIDIA helper with a **native NVIDIA direct backend** that works inside the repo’s existing headless Wayland direct-buffer environment.
- The new backend must:
  - capture frames from the same Wayland direct/screencopy path already validated by `direct_buffer_probe`
  - consume DMA-BUF frames directly
  - avoid CPU readback/copy as the normal path
  - hand frames to NVENC directly
  - emit the same encoded output shapes the server already expects:
    - H.264 Annex B
    - HEVC Annex B
    - AV1 IVF

## Implementation Advice

- Keep [cmd/server/wayland.go](/home/danial/code/LLrdc/cmd/server/wayland.go) and the existing direct-buffer probe as the source of truth for whether the compositor/runtime can expose the right Wayland protocols.
- Build the NVIDIA path around the repo's own headless Wayland session instead of introducing another monitor-capture tool that expects a real DRM-connected display.
- Make the NVIDIA helper a thin native module that does only:
  - Wayland screencopy / DMA-BUF acquisition
  - GPU import / color handling if required
  - NVENC encode
  - raw output framing for the existing server
- Keep codec/output behavior compatible with the current server so [cmd/server/ffmpeg.go](/home/danial/code/LLrdc/cmd/server/ffmpeg.go) remains mostly a routing layer.

## Required Design Constraints

- Do not change Intel direct behavior.
- Do not route Intel direct through the new NVIDIA code.
- Do not silently fall back from NVIDIA direct to the old CPU-heavy shared path.
- If the native NVIDIA direct backend cannot initialize, NVIDIA direct must fail closed with a clear reason.

## Suggested Module Layout

- Keep [cmd/server/ffmpeg.go](/home/danial/code/LLrdc/cmd/server/ffmpeg.go) as the routing layer only.
- Replace the current helper internals with dedicated NVIDIA-native modules, for example:
  - `cmd/nvidia_direct_capture/wayland_capture.go`
  - `cmd/nvidia_direct_capture/dmabuf.go`
  - `cmd/nvidia_direct_capture/nvenc.go`
  - `cmd/nvidia_direct_capture/output.go`
- If the native bindings or interop layer become too large for one helper, split them into an internal package such as `cmd/nvidia_direct_capture/internal/...` instead of pushing complexity back into the server.
- Keep [cmd/nvidia_direct_capture/config.go](/home/danial/code/LLrdc/cmd/nvidia_direct_capture/config.go) as the helper config surface unless the flag contract needs to expand.

## Runtime Reporting Fixes

- [cmd/server/direct_buffer.go](/home/danial/code/LLrdc/cmd/server/direct_buffer.go) currently reports NVIDIA direct as validated based on image/backend identity.
- That is too optimistic.
- `zeroCopyValidated` should only become `true` after the NVIDIA-native backend proves:
  - the capture path initialized
  - DMA-BUF import succeeded
  - encode started successfully
  - frames are actually flowing
- The server should expose a precise failure reason when initialization fails.
- A backend name such as `nvidia-native` is better than `nvidia-gsr` once the real implementation exists.

## Verification Order

1. Make the native helper initialize and fail with precise reasons.
2. Make [tests/nvidia/wayland_direct_buffer.spec.ts](/home/danial/code/LLrdc/tests/nvidia/wayland_direct_buffer.spec.ts) pass with decoded frames advancing.
3. Only after streaming works, rerun [tests/nvidia/wayland_direct_buffer_benchmark.spec.ts](/home/danial/code/LLrdc/tests/nvidia/wayland_direct_buffer_benchmark.spec.ts).
4. Re-check Intel direct tests to confirm no regression.

## Test Work Needed

- Keep the existing Intel direct tests unchanged.
- Update NVIDIA direct tests so they only expect:
  - `backend` to be NVIDIA-native
  - `zeroCopyValidated=true` after real backend activation
  - decoded frames > 0
- Add a negative NVIDIA direct startup test for native-backend initialization failure if possible.
- Re-run:
  - [tests/nvidia/wayland_direct_buffer.spec.ts](/home/danial/code/LLrdc/tests/nvidia/wayland_direct_buffer.spec.ts)
  - [tests/nvidia/wayland_direct_buffer_benchmark.spec.ts](/home/danial/code/LLrdc/tests/nvidia/wayland_direct_buffer_benchmark.spec.ts)

## Acceptance Criteria

- NVIDIA direct stream starts end to end in the current headless Wayland test environment.
- No fallback to `wf-recorder` is used for NVIDIA direct mode.
- Intel direct path behavior remains unchanged.
- The direct benchmark shows materially lower CPU than the old NVIDIA direct wrapper path.
- The reported direct backend/validation fields match the actual runtime state.

## Immediate Cleanup Recommendation

- Until the native backend exists, treat the current `gpu-screen-recorder` attempt as experimental.
- Do not rely on `backend: nvidia-gsr` or `zeroCopyValidated: true` as proof of working zero-copy behavior.
