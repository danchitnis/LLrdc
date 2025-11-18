# Step 2 – Make the Wayland session interactive

Now that Step 1 proved we can spin up a wlroots-based compositor and capture frames, Step 2 focuses on user interaction. We stand up a headless sway session, launch a GUI app inside it, inject keystrokes with `wtype`, and grab a screenshot that proves text made it into the window.

## 1. Install the extra tooling

```bash
sudo apt install -y sway grim wtype gedit
```

`wtype` gives us a Wayland-native equivalent of `xdotool`. If you do not want to install `gedit`, set `DEMO_APP` (see below) to another Wayland client such as `weston-terminal`.

## 2. How the interactive harness works

The repo now contains `step2/scripts/interactive-demo.js`, a Node script that:

- provisions a private `XDG_RUNTIME_DIR` (path stored in `step2/.temp/xdg-runtime-dir.txt` for debugging),
- launches `sway --debug` with the wlroots headless backend and symlinks its socket to `remote-desktop-2`,
- spawns a GUI app inside that nested session (`gedit` by default),
- types `Hello from remote desktop step 2!` into the focused window via `wtype`, and
- captures the resulting frame with `grim`, saving it to `step2/.temp/step2-demo.png`.

Cleanup is automatic—both the compositor and the demo app are terminated even if the script fails.

## 3. Run the demo

```bash
cd step2
npm run demo
```

Once the script completes you can open `step2/.temp/step2-demo.png` to confirm the text landed in the window. The file `step2/.temp/xdg-runtime-dir.txt` tells you which `XDG_RUNTIME_DIR` was used if you need to attach additional Wayland clients manually.

### Useful environment variables

- `DEMO_APP` – override the GUI program to launch (default: `gedit`). Example: `DEMO_APP=weston-terminal npm run demo`.
- `DEMO_TEXT` – replace the text that gets typed into the window.
- `WAYLAND_SOCKET` – customize the socket name exported inside the nested session (default: `remote-desktop-2`).
- `LOCAL_TMP_DIR` / `OUTPUT_PATH` – control where artifacts such as the screenshot and runtime-dir file are written.

## 4. Where to go next

This demo currently injects keyboard input via `wtype`. wlroots also offers `virtual-pointer` support, so the next iteration can extend the Node harness with pointer events (clicks, motion) and expose the input primitives over a WebSocket or RPC boundary so remote clients can drive the compositor in real time.
