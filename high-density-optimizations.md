# High-Density Remote Desktop Optimizations — Analysis for LLrdc

## Current Architecture

LLrdc's current pipeline matches the "naive architecture" described in suggestion #1:

```
Xvfb (virtual framebuffer)
     ↓
FFmpeg x11grab (CPU copy from X11 shared memory)
     ↓
[optional: hwupload_cuda for NVENC]
     ↓
Encode (libvpx / libx264 / libaom-av1 / h264_nvenc / av1_nvenc)
     ↓
pipe:1 → Go process reads stdout
     ↓
WebRTC track + WebSocket broadcast
```

Key files: [ffmpeg.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go), [x11.go](file:///home/danial/code/LLrdc/cmd/server/x11.go), [webrtc.go](file:///home/danial/code/LLrdc/cmd/server/webrtc.go), [http.go](file:///home/danial/code/LLrdc/cmd/server/http.go)

---

## Optimization Analysis

### 1. Zero-Copy GPU Capture (NvFBC / DMA-BUF)

| | |
|---|---|
| **Applies?** | ⚠️ Partially |
| **Benefit** | High — eliminates the biggest latency and bandwidth bottleneck |
| **Current state** | FFmpeg `x11grab` copies pixels from Xvfb's shared memory to userspace on the CPU. With NVENC, an additional `hwupload_cuda` step copies them CPU→GPU. |
| **Complexity** | 🔴 Very High |

**Analysis:** This is the single most impactful optimization but also the hardest to implement in LLrdc's architecture.

- **NvFBC** (NVIDIA Framebuffer Capture) captures directly from GPU framebuffers with zero CPU copy. However, it requires a *real* GPU-rendered display, not Xvfb. LLrdc uses Xvfb (software rendering), so NvFBC has nothing to capture. To use NvFBC, the entire display server would need to change from Xvfb to a GPU-backed X server or Wayland compositor — a fundamental architecture redesign.

- **DMA-BUF on Linux** enables sharing memory buffers between GPU and other devices without copying. Similar problem: requires a GPU-rendered framebuffer to export. FFmpeg does support DMA-BUF import via `vaapi` or `cuda` mechanisms, but again only if the source produces DMA-BUF handles. Xvfb does not.

- **FFmpeg `kmsgrab`** can capture DRM/KMS framebuffers as DMA-BUFs and feed them directly to NVENC without CPU copies. But this requires a DRM display, not Xvfb.

**What would need to change:**

1. Replace Xvfb with a GPU-backed display server (e.g., Xorg with `modesetting`/`nvidia` driver, or a Wayland compositor like `weston` on a virtual GPU via `nvidia-drm`)
2. Use FFmpeg `kmsgrab` or NvFBC SDK directly instead of `x11grab`
3. The Docker container would require a fundamentally different GPU passthrough architecture
4. All of [x11.go](file:///home/danial/code/LLrdc/cmd/server/x11.go) and the FFmpeg input pipeline in [ffmpeg.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go) would need rewriting

> [!CAUTION]
> This is a major architectural change that would make the CPU-only Docker path (a key feature of LLrdc) much harder to support. It would essentially fork the project into two very different deployment models. Recommend deferring unless multi-stream density becomes a hard requirement.

---

### 2. Dirty-Region Encoding

| | |
|---|---|
| **Applies?** | ✅ Yes |
| **Benefit** | Medium–High — especially for desktop/text workloads |
| **Current state** | LLrdc has `mpdecimate` filter (drops identical frames) but no spatial dirty-region detection. |
| **Complexity** | 🟡 Medium |

**Analysis:** LLrdc already has the *temporal* component (`mpdecimate` drops duplicate frames entirely) but misses the *spatial* component (encoding only the changed regions within a frame).

The spatial approach requires either:

**a) FFmpeg-level approaches (easier, moderate wins):**
- The `mpdecimate` filter already drops unchanged frames — this is the simplest form. Already implemented in [ffmpeg.go:224-228](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L224-L228).
- **`-forced_idr` + ROI encoding** with NVENC: NVENC supports region-of-interest (ROI) maps via `qp_delta_map`. Changed regions get low QP (high quality), unchanged regions get very high QP (nearly zero bits). This is achievable through FFmpeg filters or the NVENC API.
- **`-tune stillimage`** with x264 already biases toward re-using previous macroblocks, which gives some dirty-region benefit for free.

**b) Application-level dirty rectangle tracking (harder, bigger wins):**
- X11 Damage extension (`XDamage`) reports which rectangles changed since the last query. LLrdc could use this to:
  - Skip capture entirely when nothing changed (better than mpdecimate because it saves capture cost)
  - Feed damage hints to the encoder
- This would require Go CGo bindings to Xlib/XDamage, or a helper process

**What would need to change:**

*Phase 1 (quick wins):*
1. In [ffmpeg.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go) — add an `fps_mode passthrough` + frame-drop logic based on X11 Damage events to avoid capturing when nothing changed
2. For NVENC path in [ffmpeg_h264.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_h264.go) — add `-spatial-aq 1` flag which already does basic spatial quality adaptation

*Phase 2 (X11 Damage integration):*
1. New file `xdamage.go` — CGo bindings to poll XDamage for changed rectangles
2. Modify capture pipeline to skip frames when the damage region is empty
3. Optionally pipe damage rectangles as NVENC ROI hints

---

### 3. `mpdecimate` — Frame Skipping (Already Implemented)

| | |
|---|---|
| **Applies?** | ✅ Already in codebase |
| **Benefit** | Medium — reduces encoding work when screen is static |
| **Current state** | Implemented but defaults to off |
| **Complexity** | 🟢 Done |

The `mpdecimate` filter is [already implemented](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L224-L228) and togglable from the client UI. When enabled, static frames are dropped. The only suggested improvement:

- **Default it to `true`** for VBR mode (it's currently `false`)
- **Tune `mpdecimate=max=15`** — the current value of `max=15` means at least one frame every 15 will be sent even on a static screen. For desktop workloads, `max=60` (one forced frame per 2 seconds) may be better.

**What would need to change:**
1. In [ffmpeg.go:17](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L17) — consider changing default to `true`
2. Optionally expose the `max` parameter as a configurable value

---

### 4. Multi-Stream GPU Scheduling

| | |
|---|---|
| **Applies?** | ⚠️ Not yet relevant |
| **Benefit** | High for multi-instance deployments |
| **Current state** | Single-session architecture |
| **Complexity** | 🟡 Medium (when needed) |

**Analysis:** LLrdc currently runs one desktop session per container. Multi-stream scheduling is relevant when running many LLrdc instances sharing one GPU.

This "just works" with NVENC at the hardware level — multiple FFmpeg processes can each open an NVENC session and the GPU schedules them across its encoding engines. No code changes needed in LLrdc itself.

**What would matter for scaling:**
- A container orchestration layer (e.g., Kubernetes with NVIDIA device plugin) to schedule multiple LLrdc containers per GPU
- Monitoring NVENC session count (`nvidia-smi` reports this)
- Setting per-container bitrate limits to avoid saturating the encoder

> [!NOTE]
> No code changes needed in LLrdc. This is an operational/deployment concern. NVIDIA GPUs support 3+ simultaneous NVENC sessions by default (unlimited with driver patch). The time-slicing happens in hardware.

---

### 5. Memory Bandwidth / PCIe as Bottleneck

| | |
|---|---|
| **Applies?** | ✅ Yes (for NVENC path) |
| **Benefit** | Medium |
| **Current state** | CPU→GPU copy via `hwupload_cuda` on every frame |
| **Complexity** | 🟢 Low (improvements) to 🔴 High (elimination) |

**Analysis:** In the current NVENC path, the pipeline is:

```
Xvfb shared memory → FFmpeg CPU read → hwupload_cuda (PCIe copy) → NVENC
```

This means every 4K frame (≈24 MB for YUV420 or ≈33 MB for RGB) crosses the PCIe bus. At 30fps, that's ~720 MB/s to ~1 GB/s of PCIe bandwidth per stream — significant but within PCIe 3.0 x16 capacity (≈15 GB/s).

**Quick wins without architecture changes:**
1. **Ensure `format=nv12` conversion happens on GPU** — currently the filter chain is `scale=...,hwupload_cuda`, which means format conversion happens on CPU. Changing to `hwupload_cuda,scale_cuda=format=nv12` would move this to GPU.
2. **Use `hwupload_cuda` with `derive_device`** to minimize PCIe overhead.

**What would need to change:**
1. In [ffmpeg.go:236](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L236) — reorder filters to minimize CPU-side processing before the upload:
   ```
   hwupload_cuda,scale_cuda=trunc(iw/2)*2:trunc(ih/2)*2:format=nv12
   ```

---

### 6. Shared Encoding Pipeline / Compositor

| | |
|---|---|
| **Applies?** | ⚠️ Not in current architecture |
| **Benefit** | High for cloud gaming clusters |
| **Current state** | 1 container = 1 session = 1 encoder |
| **Complexity** | 🔴 Very High |

**Analysis:** This optimization is about running a compositor that merges multiple virtual desktops into tiles and encodes them as a single stream. This is architecturally incompatible with LLrdc's "one container, one desktop" model and is more relevant for cloud gaming infrastructure.

> [!NOTE]
> Not recommended for LLrdc. The container-per-session model provides better isolation, simpler deployment, and matches the project's philosophy.

---

### 7. Best Architecture (DMA-BUF Capture)

See #1 above. Same analysis applies. The ideal pipeline of:

```
Wayland/X → DMA-BUF capture → zero-copy GPU texture → NVENC → WebRTC/UDP
```

would require replacing Xvfb with a GPU-backed display server.

---

### 8. Hardware Selection Guidance

| | |
|---|---|
| **Applies?** | ✅ Yes (documentation recommendation) |
| **Benefit** | Informational |

LLrdc already supports NVENC. Hardware recommendations could be added to the README:

- **Consumer/Dev**: NVIDIA RTX 3060+ (2 NVENC sessions without driver patch)
- **Data center**: NVIDIA L4/T4 (designed for multi-stream)
- **Budget**: Intel Arc A380 (AV1 encode, but would need `vaapi` encoder support in FFmpeg pipeline, not currently supported)

**What would need to change:**
1. Add a "Recommended Hardware" section to [README.md](file:///home/danial/code/LLrdc/README.md)
2. For Intel Arc support — add `h264_vaapi` / `av1_vaapi` codec options (new work)

---

### 9. Partial-Frame / Scanline Encoding

| | |
|---|---|
| **Applies?** | ⚠️ Theoretically, but impractical with FFmpeg |
| **Benefit** | Low in current architecture |
| **Current state** | Full-frame pipeline |
| **Complexity** | 🔴 Very High |

**Analysis:** Scanline/slice-parallel encoding means starting to encode the top of a frame before the bottom is ready. This can reduce latency by almost one frame period.

- **H.264 slices**: NVENC `h264_nvenc` does support `-slices` parameter, and FFmpeg can output slice-by-slice. However, LLrdc's frame splitter in [ffmpeg_h264.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_h264.go) uses Access Unit Delimiter (AUD) boundaries, which are full-frame boundaries.
- **WebRTC**: The Pion WebRTC library sends complete samples — there's no easy way to send partial frames through `WriteSample`.
- **WebSocket path**: Could theoretically send slice data as it arrives, but the client's `VideoDecoder` (WebCodecs) also expects complete access units.

**Achievable low-hanging fruit:**
- **`-tune ull` + `-zerolatency`** is already set for NVENC in [ffmpeg_h264.go:13](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_h264.go#L13). This enables immediate output of encoded data without buffering — the encoder-level equivalent.
- **`-fflags nobuffer` + `-probesize 32`** is already set in [ffmpeg.go:260-262](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L260-L262).

> [!TIP]
> The existing `-tune ull -zerolatency` and `-fflags nobuffer` settings already capture most of the latency benefit of this suggestion. True scanline-level streaming would require bypassing FFmpeg and using the NVENC SDK directly, which is a massive architectural change.

---

### 10. YUV 4:4:4 for Text/CAD

| | |
|---|---|
| **Applies?** | ✅ Yes |
| **Benefit** | High for text readability and sharp edges |
| **Current state** | Always YUV 4:2:0 |
| **Complexity** | 🟡 Medium |

**Analysis:** LLrdc forces `format=yuv420p` for CPU codecs ([ffmpeg.go:242](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go#L242)). Chroma subsampling (4:2:0) noticeably blurs colored text and sharp edges, which is the primary use case for remote desktops.

- **NVENC H.264/AV1** supports 4:4:4 encoding via `highyuv444p` profile.
- **libx264** supports 4:4:4 with `-profile:v high444` and `format=yuv444p`.
- **VP8** does NOT support 4:4:4 at all.
- **Client decoding**: WebCodecs `VideoDecoder` supports High 4:4:4 Profile with codec string `avc1.F40034`. WebRTC may have browser-dependent support.

**What would need to change:**

1. In [ffmpeg.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg.go) — add a `targetChromaFormat` setting (420 vs 444) and wire it to the filter chain:
   - CPU path: `format=yuv444p` instead of `format=yuv420p`
   - NVENC path: remove `hwupload_cuda` auto-format, pass `yuv444p` explicitly
2. In [ffmpeg_h264.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_h264.go) — add `-profile:v high444` for H.264; for NVENC use `highyuv444p`
3. In [webcodecs.ts](file:///home/danial/code/LLrdc/src/webcodecs.ts) — change codec string from `avc1.42E034` (Baseline) to `avc1.F40034` (High 4:4:4 Predictive) when 4:4:4 mode is active
4. In [config.go](file:///home/danial/code/LLrdc/cmd/server/config.go) — add `--chroma` flag
5. Client UI — add a "Chroma 4:4:4" toggle

> [!IMPORTANT]
> This is one of the highest-impact, most practical improvements for desktop/text use cases. It's moderate complexity and stays within the current FFmpeg pipeline architecture. However, it increases bitrate by ~50% compared to 4:2:0 and requires H.264 High profile or AV1 (VP8 does not support 4:4:4), and WebRTC browser support may be limited.

---

### 11. NVENC `-spatial-aq` (bonus — not in original list)

| | |
|---|---|
| **Applies?** | ✅ Yes |
| **Benefit** | Low–Medium |
| **Current state** | Not enabled |
| **Complexity** | 🟢 Very Low |

NVENC's spatial adaptive quantization allocates more bits to complex regions and fewer to flat areas. This is a one-line change per codec file.

**What would need to change:**
1. In [ffmpeg_h264.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_h264.go) — add `-spatial-aq 1` to NVENC args
2. In [ffmpeg_av1.go](file:///home/danial/code/LLrdc/cmd/server/ffmpeg_av1.go) — add `-spatial-aq 1` to NVENC args

---

## Summary & Priority Matrix

| # | Optimization | Applies? | Benefit | Effort | Recommendation |
|---|---|---|---|---|---|
| 10 | **YUV 4:4:4** | ✅ | 🟢 High | 🟡 Medium | **Do it** — biggest visual improvement for desktop use |
| 11 | **NVENC `-spatial-aq`** | ✅ | 🟡 Medium | 🟢 Low | **Do it** — one-line change, free quality improvement |
| 3 | **`mpdecimate` tuning** | ✅ | 🟡 Medium | 🟢 Low | **Do it** — already implemented, just tune defaults |
| 5 | **GPU filter reordering** | ✅ | 🟡 Medium | 🟢 Low | **Do it** — move format conversion to GPU side |
| 2 | **Dirty-region (X11 Damage)** | ✅ | 🟢 High | 🟡 Medium | **Plan for Phase 2** — requires CGo + X11 bindings |
| 8 | **Intel Arc/VAAPI support** | ✅ | 🟡 Medium | 🟡 Medium | **Consider** — broadens hardware support |
| 1,7 | **Zero-copy / DMA-BUF** | ⚠️ | 🟢 High | 🔴 High | **Defer** — requires replacing Xvfb entirely |
| 9 | **Partial-frame encoding** | ⚠️ | 🟡 Medium | 🔴 High | **Skip** — already covered by `-tune ull` settings |
| 4 | **Multi-stream scheduling** | ⚠️ | 🟡 Medium | 🟢 Low | **No code changes** — works automatically with NVENC |
| 6 | **Shared compositor pipe** | ❌ | 🟡 Medium | 🔴 High | **Skip** — incompatible with container model |

## Verification Plan

This is a research/analysis document — no code changes are proposed at this time. If the user chooses to proceed with specific optimizations, each will get its own detailed implementation plan with verification steps.
