# Step 3 – Periodic screenshots over the network

Step 2 gave us interactive control inside the nested Wayland session. To keep things simple, Step 3 sticks with still images instead of full-motion video. We poll the compositor every N milliseconds, send PNG frames over a websocket, and allow a remote viewer to inject keyboard commands back to the server.

## Objectives

1. **Screenshot loop**
   - Reuse `grim` but call it on a timer (e.g., 2 FPS) and save each frame to an in-memory buffer instead of disk.
2. **Transport**
   - Run a lightweight Node WS server that broadcasts PNG frames to connected clients. Frames can be base64-encoded or sent as binary blobs.
3. **Input channel**
   - Extend the existing `wtype` pipeline to accept key events sent by the client via websocket messages.
4. **Viewer**
   - Provide a barebones HTML page or CLI script that connects to the websocket, displays incoming PNGs, and forwards keyboard events.

## Deliverables

- `step3/` folder with:
  - `scripts/server.js` that launches the compositor (reuse Step 2), starts the screenshot loop, serves frames over websockets, and relays keyboard input.
  - `public/viewer.html` (or similar) that renders the incoming PNG stream and sends key events back.
- Documentation (`step3.md`) explaining how to start the server (`npm run step3-server`), open the viewer, configure FPS interval, and what resolution/latency to expect.
- Success criteria: open the viewer from another terminal/browser, see the nested session update every few seconds, and type characters that appear inside the nested app.
