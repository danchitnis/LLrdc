# Step 1 – Bring up a Wayland session and test pipe

The goal of this step is to boot a disposable Wayland compositor for experimentation and prove we can capture a frame through a FIFO pipe before attempting any production code. All commands below run on the capture host.

## 1. Install the tools you need

```bash
sudo apt install -y sway grim nodejs npm
```

`sway` (a wlroots compositor) gives us a self-contained Wayland session with `zwlr_screencopy` support, `grim` captures screenshots, and Node.js drives the test pipe.

## 2. Launch a nested Wayland session

The Node harness launches a headless `sway` instance under the hood (using the wlroots headless backend) so you no longer need to manually start a compositor. If you want to run it by hand for debugging, the equivalent command looks like:

```bash
WLR_BACKENDS=headless \
WLR_LIBINPUT_NO_DEVICES=1 \
XDG_RUNTIME_DIR="$(mktemp -d)" \
sway --debug &
```

Within that temporary `XDG_RUNTIME_DIR`, a single Wayland socket is created (for example `wayland-0`). The script symlinks it to `remote-desktop-1` so clients can simply set `WAYLAND_DISPLAY=remote-desktop-1`.

## 3. Create a FIFO that represents the capture pipe

```bash
PIPE_PATH=/tmp/remote-desktop-frame.pipe
rm -f "$PIPE_PATH"
mkfifo "$PIPE_PATH"
```

Anything that writes a frame to `PIPE_PATH` now behaves like our future encoder input. Multiple readers are not supported, so keep tests single-consumer.

## 4. Use Node.js to validate the pipeline

The repo already includes `step1/scripts/test-frame.js`, a small Node program that:

- checks that `sway` (or whichever compositor you set via `WAYLAND_COMPOSITOR`), `grim`, and `mkfifo` are installed,
- ensures the FIFO at `/tmp/remote-desktop-frame.pipe` exists,
- launches `sway` headless with its own Wayland socket (and waits for it to be ready),
- runs `grim` against that socket and writes into the FIFO, and
- reads from the FIFO to `step1/.temp/remote-desktop-test.png` (created automatically) so we can confirm the pipe delivers real frame data.

You can switch compositors by exporting `WAYLAND_COMPOSITOR=sway` (default) or `WAYLAND_COMPOSITOR=weston` before running the script. Note that Weston does not implement the `zwlr_screencopy` protocol used by `grim`, so only wlroots-based compositors (like `sway`) produce screenshots.

From the repo root, run it:

```bash
cd step1
npm run test-frame
```

`grim` grabs a frame from the nested Wayland session, writes it into the FIFO, and the Node.js process streams it into `step1/.temp/remote-desktop-test.png`. Opening the PNG (or hashing it) confirms the pipe is wired correctly.

## 5. Clean up

Stop the compositor when you are done (the script handles this automatically, but for manual experiments):

```bash
pkill -f 'sway --debug'
rm -f /tmp/remote-desktop-frame.pipe step1/.temp/remote-desktop-test.png
```

You now have a deterministic Wayland environment plus a Node-driven FIFO test to validate the rest of the remote desktop pipeline.
