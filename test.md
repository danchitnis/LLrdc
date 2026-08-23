# LLrdc test catalog

This is the authoritative guide to the maintained tests. The primary Linux browser lane is the August connection smoke: one CPU, one NVIDIA, and one Intel spec. Older March–May Linux browser suites were removed because they duplicated lifecycle logic or asserted obsolete transport and codec states.

## Surface matrix

| Test surface | Server | Headed browser/client | Transport |
| --- | --- | --- | --- |
| CPU connection | local Docker | local macOS or nzxt5 Wayland session | WebTransport |
| NVIDIA connection | prestarted nzxt5 | local macOS or nzxt5 Wayland session | HTTPS + WebTransport |
| Intel connection | prestarted nzxt5 | local macOS or nzxt5 Wayland session | HTTPS + WebTransport |
| macOS split | local native macOS server + local Docker capture agent | installed headed Chrome, then installed headed Safari | Chrome: WebTransport; Safari: WebSocket |
| Native Wayland | local or nzxt5 | native SDL/Wayland client | WebTransport |

## Prerequisites and synchronization

- Node.js/npm dependencies are installed (`npm install`), Docker is running, and the checkout is on `next`.
- Linux/GPU browser tests use the installed, headed Google Chrome binary. Playwright's bundled Chromium is not used; headless Chrome is rejected.
- The macOS split lane requires `/Applications/Google Chrome.app` and `/usr/bin/safaridriver`. It never launches Playwright Chromium or Playwright WebKit. Enable Safari Develop/Developer Settings → Allow Remote Automation once before the first run (or run `safaridriver --enable`). Chrome uses WebTransport; Safari is intentionally pinned to WebSocket until the Safari WebTransport path is enabled for this lane.
- The Linux browser lane needs an active Wayland session and `XDG_RUNTIME_DIR`/`WAYLAND_DISPLAY` on nzxt5.
- For remote work, synchronize the macOS checkout to nzxt5 with SCP before running there. Preserve and inspect remote changes first; the sync skill's archive-overlay workflow is the supported method:

  ```bash
  tar --exclude=.git --exclude=node_modules --exclude=.artefact -czf /tmp/llrdc-overlay.tgz .
  scp /tmp/llrdc-overlay.tgz nzxt5:/tmp/
  ssh nzxt5 'mkdir -p ~/code/LLrdc && tar -xzf /tmp/llrdc-overlay.tgz -C ~/code/LLrdc'
  ```

  Do not use a separate SSH test script: invoke the same runner through `ssh` when the browser is on nzxt5.

## HTTPS and certificate behavior

GPU runners keep the HTTP port for `/readyz`, but navigate Chrome to `https://<server-host>:<port+10>/` and require WebTransport. The shared Playwright helper tolerates only Chrome's recognized self-signed certificate interstitial: it clicks `#details-button`, then `#proceed-link`. If Chrome has already accepted that certificate, the viewer loads directly. Missing controls or unrelated TLS/navigation failures are hard errors. `ignoreHTTPSErrors`, insecure-origin flags, and WebTransport disabling are not used.

## CPU connection smoke

The runner builds/starts one CPU container, runs `tests/linux-cpu-browser/wayland_minimal.spec.ts`, and cleans that container on exit. It accepts no spec arguments, so the same command works locally or through SSH:

```bash
npm test
npm run test:cpu
npm run test:cpu:connection
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && npm run test:cpu:connection"'
```

## NVIDIA and Intel browser connections

The GPU server is prestarted and owned by the caller. Run one capture mode per server instance. Build the image on nzxt5 first:

```bash
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && ./docker-build.sh --nvidia"'
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && ./docker-build.sh --intel"'
```

Start NVIDIA in `compat` (replace `compat` with `direct` for the direct lane):

```bash
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && PORT=8080 HOST_PORT=8080 VIDEO_CODEC=h264_nvenc ./docker-run.sh --nvidia --capture-mode compat --host-net --detach --name llrdc-nvidia-browser"'
```

Start Intel in `compat` (replace `compat` with `direct`; `D130` is the default render node):

```bash
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && PORT=8080 HOST_PORT=8080 VIDEO_CODEC=h264_vaapi ./docker-run.sh --intel --intel-device D130 --capture-mode compat --host-net --detach --name llrdc-intel-browser"'
```

Run the headed browser locally on macOS, or run the identical runner over SSH for the nzxt5 browser:

```bash
npm run test:nvidia:connection -- --capture-mode compat
npm run test:nvidia:connection -- --capture-mode direct
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && ./test-nvidia-browser.sh --capture-mode compat"'

npm run test:intel:connection -- --capture-mode compat
npm run test:intel:connection -- --capture-mode direct
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && ./test-intel-browser.sh --capture-mode compat"'
```

Use `--server-host`, `--port`, and Intel's `--render-node D130` when the endpoint differs. Every run must report the exact accelerator, codec, capture mode, active WebTransport connection, sustained H.264 decoding, positive FPS, and advancing decoded-frame count. Direct mode additionally requires supported/active direct capture and `zeroCopyValidated=true`; it never falls back to compat.

## macOS split browser suite

This is a macOS-only local test. It starts the native macOS VideoToolbox server and its local Docker Desktop capture/input agent. It does not use nzxt5, SSH, SCP, or an external Linux host. The runner builds once, then runs the maintained scenarios in installed headed Chrome followed by Safari. Each browser/scenario pair gets a fresh server, capture container, and browser session.

```bash
npm run test:macos:browser
./test-macos-split.sh --browser chrome
./test-macos-split.sh --browser safari
./test-macos-split.sh --scenario connection
./test-macos-split.sh --browser safari --scenario codecs
```

Chrome scenarios are `connection`, `reconfiguration`, `resolution`, `hdpi`, `input`, `clipboard`, and `codecs`. Safari scenarios are `connection`, `reconfiguration`, `resolution`, `hdpi`, `input`, and the H.265 4:4:4 codec check. Safari clipboard synchronization and H.264 4:4:4 are not implemented, so they are intentionally not run. Chrome requires WebTransport; Safari requires WebSocket for now. The wrong transport, certificate/navigation failure, missing Safari automation, unsupported decoder, or non-advancing frame stream fails the scenario. Failure artifacts are stored under `.artefact/macos-browser/<browser>-<scenario>/` and include the server log, container log, and a browser screenshot. The runner always closes the active browser and removes only the server/container it started.

The Safari HDPI scenario uses a fixed headed window that maps to a 1920x1072 capture surface at Safari's 2x pixel ratio, then verifies the compositor reaches 200% scale and frames continue advancing.

The viewer is loaded from local HTTP. Chrome's WebTransport path uses the native server's HTTPS listener at HTTP port + 10 and its persisted certificate fingerprint; Safari uses the viewer's local WebSocket endpoint. The macOS suite does not inject insecure-origin/browser fallback flags.

## Native package and latency tests

```bash
npm run client:test
npm run client:verify-package
npm run test:latency:cpu
npm run test:latency:intel
npm run test:latency:nvidia
ssh nzxt5 'bash -lic "cd ~/code/LLrdc && ./scripts/benchmark-latency.sh --help"'
```

The latency wrapper is `scripts/benchmark-latency.sh`; use `--accel cpu|intel|nvidia`, `--mode compat|direct`, `--samples N`, and `--skip-build` as appropriate. Native tests require a visible, focused Wayland destination and report compositor presentation feedback rather than photon timing.

## Retained focused diagnostics

These scripts remain useful for targeted investigation but are deliberately separate from the primary suite: `scripts/test-macos-native-444.sh`, `scripts/test-macos-native-bitrate.sh`, `scripts/probe-macos-native-dimensions.sh`, `scripts/test_inactivity_delay.py`, `scripts/verify-codec-switch-native.sh`, and `scripts/verify-native-client-package.sh`. Run them only when diagnosing the specific behavior they cover.

## Artifacts and cleanup

Playwright writes its normal report/output under the configured test output directory; latency reports are written under `.artefact/<accelerator>/`. GPU runners never build, start, stop, or SSH into a server. Stop the prestarted GPU container explicitly after each mode, for example `ssh nzxt5 'docker rm -f llrdc-nvidia-browser'`. CPU and macOS runners clean up containers they create. Preserve useful logs and artifacts before cleanup when diagnosing a failure.
