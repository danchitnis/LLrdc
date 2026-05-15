# Browser vs Native ULL Latency Results

Test date: `2026-04-27`

Tested base commit:

```text
a27ae7775283c31972c5da246cfff3588894f4a4
```

Short commit: `a27ae77`

Commit subject:

```text
fix(server): update default XFCE background to 4.20 default (xfce-x.svg)
```

The working tree included uncommitted benchmark/test changes when these measurements were taken. Use the full commit SHA above as the stable base reference; branch names such as `macos` may move over time.

This report compares the browser WebRTC ULL path with the native Linux client ULL benchmark.

## Test Configuration

### Browser ULL

Command:

```bash
LLRDC_CLOCK_SYNC_MAX_RTT_MS=100 ./scripts/run-browser-ull-latency.sh
```

Server/app side:

- Docker-built server image: `danchitnis/llrdc:latest`
- Standard mode: no WebRTC low-latency flag
- ULL mode: `--webrtc-low-latency --webrtc-buffer 0`
- Codec: `vp8`
- Target FPS: `60`
- Resolution: `1280x720`
- Bandwidth cap: `10 Mbps`

Browser/test-driver side:

- Playwright browser runs on the host.
- The browser is not run inside Docker.
- Measured samples: `30` per mode
- Warmup samples: `5` per mode

Artifact:

```text
test-results/browser-ull-latency/webrtc-ull-comparison.json
```

### Native ULL

Command:

```bash
WEBRTC_LOW_LATENCY=true \
WEBRTC_BUFFER_SIZE=0 \
LLRDC_VIDEO_CODEC=vp8 \
LLRDC_ARTIFACT_DIR=/tmp/llrdc-native-latency-ull-default \
./tests/linux-wayland-native/benchmark-wayland-native-latency.sh
```

Native benchmark configuration:

- WebRTC ULL enabled: `WEBRTC_LOW_LATENCY=true`
- WebRTC buffer size: `WEBRTC_BUFFER_SIZE=0`
- Codec: `vp8`
- Resolution: `1280x720`
- Target FPS: `60`
- Measured samples: `5`
- Warmup samples: `3`

Artifact:

```text
/tmp/llrdc-native-latency-ull-default/latency-report.txt
```

## Browser Standard vs Browser ULL

Browser benchmark, `30` measured samples per mode:

| Metric | Standard median | ULL median | ULL delta |
|---|---:|---:|---:|
| Input to browser callback | `95.80 ms` | `95.90 ms` | `+0.10 ms` |
| Remote draw to browser callback | `48.70 ms` | `48.05 ms` | `-0.65 ms` |
| Decode-ready to compose | `27.20 ms` | `30.00 ms` | `+2.80 ms` |
| Remote request to draw | `12.00 ms` | `11.00 ms` | `-1.00 ms` |
| Remote draw to first frame broadcast | `5.00 ms` | `5.00 ms` | `0.00 ms` |

Browser RTP timestamp matching:

| Mode | Exact RTP matches | Visual fallback matches |
|---|---:|---:|
| Standard | `30/30` | `0/30` |
| ULL | `30/30` | `0/30` |

Observed browser jitter-buffer delay:

| Mode | Jitter buffer delay |
|---|---:|
| Standard | `11.18 ms` |
| ULL | `9.41 ms` |

## Native ULL Result

Native ULL benchmark, `5` measured samples:

| Metric | Native ULL |
|---|---:|
| Total E2E min | `19 ms` |
| Total E2E median | `22 ms` |
| Total E2E mean | `21.40 ms` |
| Total E2E p90 | `23 ms` |
| Native client stage median | `2 ms` |

Native per-sample totals:

| Marker | Total E2E | Render | PostDraw+Encode | Client | Present source |
|---:|---:|---:|---:|---:|---|
| `4` | `19 ms` | `16 ms` | `1 ms` | `2 ms` | `render_present` |
| `5` | `22 ms` | `13 ms` | `5 ms` | `2 ms` | `render_present` |
| `6` | `22 ms` | `14 ms` | `4 ms` | `2 ms` | `render_present` |
| `7` | `23 ms` | `18 ms` | `2 ms` | `2 ms` | `render_present` |
| `8` | `21 ms` | `14 ms` | `5 ms` | `2 ms` | `render_present` |

## Browser ULL vs Native ULL

| Path | Median latency |
|---|---:|
| Native ULL total E2E | `22 ms` |
| Browser ULL input to browser callback | `95.90 ms` |
| Browser ULL remote draw to browser callback | `48.05 ms` |

The browser ULL path measured about `73.90 ms` slower than native ULL at median when comparing browser input-to-callback against native total E2E:

```text
95.90 ms - 22.00 ms = 73.90 ms
```

The largest visible difference is on the client presentation side:

| Client-side metric | Median |
|---|---:|
| Native client stage | `2 ms` |
| Browser decode-ready to compose | `30.00 ms` |
| Browser remote draw to browser callback | `48.05 ms` |

## Interpretation

In the stricter exact-RTP run, ULL did not materially improve the browser path:

- Browser `inputToBrowserCallbackMs` changed by `+0.10 ms` median.
- Browser `remoteDrawToBrowserCallbackMs` improved by `0.65 ms` median.
- Browser `decodeReadyToComposeMs` regressed by `2.80 ms` median.

ULL did not materially improve full browser input-to-callback latency in this run:

- Standard browser median: `95.80 ms`
- ULL browser median: `95.90 ms`
- Difference: `+0.10 ms`

The native ULL path is much lower latency in this environment:

- Native ULL median total E2E: `22 ms`
- Browser ULL median input-to-callback: `95.90 ms`

## Reliability Notes

The browser benchmark was corrected before the final run:

- Probe JSON reads now tolerate atomic write/read races.
- Server trace timestamps are used as the authoritative remote timeline.
- Probe triggering retries the outside-to-center transition and uses click fallback.
- Measured samples require exact RTP timestamp matches. Visual-marker-only candidates are skipped and retried.

The native benchmark was first attempted with `30` measured samples to match the browser sample count, but the existing native harness failed during report collection:

```text
Missing client sample for marker 6
```

That failed run did progress through measurement, but it did not produce a valid report. The native result in this file is from the canonical native benchmark default of `5` measured samples, which completed successfully.
