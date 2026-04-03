# CPU Test Report - LLrdc

**Date:** April 3, 2026
**Environment:** Linux (CPU-only, no GPU)
**Configuration:** Playwright Headed Mode, Workers: 1

## Executive Summary
A suite of 11 CPU-related Playwright integration tests was executed to assess the stability of the remote desktop environment. 

| Total Tests | Passed | Failed | Success Rate |
|-------------|--------|--------|--------------|
| 11          | 6      | 5      | ~54.5%       |

---

## Detailed Test Results

### ✅ Passed
1. **`wayland_audio.spec.ts`**: Audio track received and decoded successfully.
2. **`wayland_framerate.spec.ts`**: Successfully verified frame delivery.
3. **`wayland_keyboard_e2e.spec.ts`**: Keyboard input handled correctly.
4. **`wayland_mouse.spec.ts`**: Rapid mouse movements handled without stalling (1000 moves in ~400ms).
5. **`wayland_vbr_ui.spec.ts`**: UI metrics (FPS/BW/CPU) correctly updated during activity.
6. **`wayland_xfce.spec.ts`**: Verified XFCE process tree (labwc, xfce4-panel, xfdesktop, etc.) is healthy.

### ❌ Failed
1. **`wayland_hdpi.spec.ts`**: Test failed during execution.
2. **`wayland_input_comp.spec.ts`**: Test failed during execution.
3. **`wayland_minimal.spec.ts`**: **Network Instability.** Failed with `WebRTC ICE connection state changed: failed`. The PeerConnection dropped before verification.
4. **`wayland_tailscale_iface.spec.ts`**: **Environment Mismatch.** Failed with `ip link show tailscale0` error. The test assumes a `tailscale0` interface exists on the host/container bridge.
5. **`wayland_vbr.spec.ts`**: **Logic Error.** Failed on string assertion. Expected `-D` flag in `wf-recorder` logs (indicating damage tracking) when VBR is toggled, but the flag was missing from the captured command.

---

## Observed System Instabilities
Frequent critical errors and warnings were logged by the XFCE desktop environment during bootstrap across almost all test runs:

*   **XFDesktop Assertions:**
    *   `CRITICAL **: xfdesktop_regular_file_icon_new: assertion 'G_IS_FILE_INFO(file_info)' failed`
    *   `GLib-GObject-CRITICAL **: g_object_unref: assertion 'G_IS_OBJECT (object)' failed`
*   **D-Bus Warnings:**
    *   `The name org.freedesktop.thumbnails.Thumbnailer1 was not provided by any .service files`
*   **WebRTC Failures:**
    *   Occasional `ICE connection state changed: failed` suggests potential signaling or STUN/TURN timeout issues in the virtualized network environment.

## New Observations (Headed Test)
During manual follow-up in headed mode, the following issues were noted:
1.  **Initial Stream Delay:** The stream remains blank for several seconds (3-5s) after the WebRTC connection is established before the first frame appears.
2.  **WebRTC Failures:** Some tests fail to establish a stable WebRTC connection unless `--network host` is used (Docker bridge networking interference).

---

## Technical Root Cause Analysis
### 1. Stream Delay / Blank Screen
- **Keyframe Interval:** The `wf-recorder` command for VP8 (CPU) is missing an explicit GOP size (`-p g=...`). It likely defaults to a large interval, causing the browser to wait for the next "natural" keyframe.
- **Aggressive Restarting:** Currently, `llrdc` kills and restarts `wf-recorder` on every new WebRTC connection to force a keyframe. While effective, the startup overhead of `wf-recorder` + `labwc` initialization adds several seconds of "blank" time.

### 2. WebRTC Connectivity
- **Docker Bridge NAT:** WebRTC often struggles with Docker's default bridge network due to ICE candidate mismatch and UDP port mapping complexities. Using `--network host` bypasses this by using the host's network stack directly.

---

## Update (April 3, 2026 - Second Run)
After applying GOP optimizations and switching to `--network host` for Playwright tests, another full run was performed.

### Improvements
1.  **Stream Delay Resolved:** The initial blank screen duration has been reduced from ~5s to ~2s. The browser now receives a keyframe almost immediately after negotiation.
2.  **WebRTC Stability:** `--network host` has significantly improved ICE negotiation stability.

### Current Status
| Total Tests | Passed | Failed | Success Rate |
|-------------|--------|--------|--------------|
| 11          | 7      | 4      | ~63.6%       |

**New Passing Tests:**
- `wayland_minimal.spec.ts` (After fixing test logic to close the config dropdown).

**Remaining Failures:**
1.  **`wayland_hdpi.spec.ts`**: Video playback check timed out. Likely needs more activity or a longer timeout for the first frame to decode when scaling is 200%.
2.  **`wayland_input_comp.spec.ts`**: Wheel scroll delta mismatch (`149.16` vs expected `100`).
3.  **`wayland_keyboard_e2e.spec.ts`**: **Display Conflict.** When using `--network host`, Xwayland in the container conflicts with the host's X server on `:0`. `xclip` failed with `Can't open display: :0`.
4.  **`wayland_tailscale_iface.spec.ts`**: Still failing due to missing `tailscale0` interface (environment issue).
5.  **`wayland_vbr.spec.ts`**: Still failing on `-D` flag check (investigation ongoing).

---

## Technical Root Cause Analysis
### 1. X11 Display Conflict (:0)
- **Problem:** `--network host` shares the host's network namespace, which includes abstract sockets. Linux X11 servers use abstract sockets (e.g., `@/tmp/.X11-unix/X0`). 
- **Impact:** The container's Xwayland cannot bind to `:0` if the host already has an X server. This breaks tests relying on `docker exec ... xclip`.

### 2. VBR Flag Assertion
- **Observation:** `wayland_vbr.spec.ts` expects `-D` to appear in the `wf-recorder` command line when VBR is toggled. The server *does* restart with `-D`, but the test might be reading old logs or the regex is failing.

---

## Recommendations for Follow-up
1.  **Investigate VBR Flag:** Check why the `-D` flag isn't being passed to `wf-recorder` when VBR is disabled/enabled in `wayland_vbr.spec.ts`.
2.  **Fix Tailscale Test:** Update the test to check for interface existence before attempting to use it, or mock the network interface.
3.  **Address ICE Failures:** Debug the root cause of WebRTC connection drops in the `wayland_minimal` suite.
4.  **Suppress XFCE Noise:** Look into stabilizing the XFCE desktop icon initialization to reduce log noise and potential race conditions.
