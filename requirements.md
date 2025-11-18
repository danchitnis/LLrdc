
# Remote Wayland Multi‑User Web Desktop – Architecture Summary

This document summarises the requirements, design criteria, architecture, and examples needed to build a **multi‑user Wayland remote desktop system** with:

- **Browser-based client (TypeScript)**
- **Wayland compositor per user**
- **PipeWire capture**
- **H.264/H.265 encoding**
- **WebSocket transport (no WebRTC)**
- **SSH/Tailscale for security**
- **Isolated sessions per Unix user**

Use this as the README for a new repo before implementing modules step‑by‑step.

---

## 🎯 Goals

1. **Multiple simultaneous users**  
   Each request gets its own isolated Wayland session (headless; no physical login required).

2. **Browser-based client**  
   - Implemented in TypeScript  
   - Uses **WebCodecs** for decoding (HEVC preferred, H.264 fallback)  
   - Renders to `<canvas>`  
   - Sends input via WebSocket (mouse, keyboard)

3. **Efficient screen encoding**  
   - Prefer **H.265 (HEVC)**  
   - Fallback to **H.264**  
   - Use hw-accelerated encoders when available (VAAPI/NVENC)

4. **Transport**  
   - Pure **HTTPS/WebSockets** (port 443)  
   - Does *not* use WebRTC  
   - Works via:
     - Direct LAN access
     - Tailscale
     - SSH port forwarding (`ssh -L`)

5. **Authentication**  
   - Login page with **username/password**
   - Backend authenticates via **PAM** (maps to Unix users)
   - Creates session cookies
   - After login, client loads remote desktop UI

6. **Server environment**  
   - Ubuntu **24.04**
   - Wayland default (GNOME or headless compositor)
   - systemd user sessions
   - PipeWire + xdg-desktop-portal for screencast & input injection

---

## 🏗 High-level Architecture

```
Browser (TS/WebCodecs)
         ↕  WebSocket (WSS)
Gateway Daemon (Rust/Go/Node)
         ↕  per-user Unix socket
Per-user Agent (Rust/C/GStreamer)
         ↕  PipeWire (frames)
Headless Wayland Session (weston/sway)
```

### Components

1. **Landing page + login**
   - `/api/login` validates via PAM
   - Sets secure cookie
   - Redirects to `/desk`

2. **Gateway**
   - Authenticates WebSocket via cookie
   - Maps session → Unix user
   - Ensures user's Wayland session is running
   - Forwards encoded video → client
   - Forwards keyboard/mouse → agent

3. **Session Manager**
   - Starts/stops per-user Wayland sessions
   - Uses systemd user services
   - Creates a compositor + PipeWire + agent

4. **Per-user Agent**
   - Requests PipeWire screencast stream via xdg-desktop-portal
   - Captures frames from PipeWire
   - Encodes to H.265/H.264
   - Sends NAL units to gateway
   - Accepts pointer/keyboard injection commands

5. **Browser TS Client**
   - Negotiates codec
   - Receives frames over WebSocket
   - Decodes using WebCodecs
   - Draws onto `<canvas>`
   - Captures input and sends back

---

## 🧩 Detailed Components

### 1. Wayland Session (headless)

Each Unix user has a systemd user service:

`~/.config/systemd/user/wayrd-session.service`:

```
[Unit]
Description=Headless Wayland Remote Desktop
After=graphical-session.target pipewire.service

[Service]
Environment=WAYLAND_DISPLAY=wayland-0
ExecStart=/usr/local/bin/wayrd-session-start.sh
Restart=on-failure

[Install]
WantedBy=default.target
```

`wayrd-session-start.sh`:

```
#!/usr/bin/env bash
weston --backend=headless-backend.so --socket="$WAYLAND_DISPLAY" &
/usr/local/bin/wayrd-agent &
xfce4-session &
wait
```

---

### 2. Agent Responsibilities

- Use **xdg-desktop-portal screencast/remote-desktop API**  
- Bind to PipeWire stream  
- Encode frame → H.265 (or H.264)  
- Provide encoded stream via Unix socket:

```
/run/user/$UID/wayrd.sock
```

- Receive input JSON:
  - Pointer movement
  - Button presses
  - Key presses  
- Inject via remote-desktop portal

---

### 3. Gateway Responsibilities

- Expose:
  - `POST /api/login`
  - `GET /desk`
  - `GET /ws` (WebSocket)
- Authenticate user via PAM
- Create session cookies
- For WebSocket:
  - Identify user from cookie
  - Ensure session running via:
    ```
    sudo -u username systemctl --user start wayrd-session
    ```
  - Connect to `/run/user/$UID/wayrd.sock`
  - Proxy:
    - Agent → client (binary video frames)
    - Client → agent (JSON input events)

---

### 4. Browser Client (TS)

#### Codec negotiation

```ts
await VideoDecoder.isConfigSupported({
  codec: "hev1.1.6.L120.B0",
  codedWidth: 1920,
  codedHeight: 1080,
});
```

Fallback to H.264 if unsupported.

#### WebSocket frame handling (H.26x annex-B)

```ts
ws.onmessage = (ev) => {
  const data = new Uint8Array(ev.data);
  const chunk = new EncodedVideoChunk({
    type: "key",
    timestamp: BigInt(pts),
    data,
  });
  decoder.decode(chunk);
};
```

#### Input events

```ts
canvas.addEventListener("pointermove", e => {
  ws.send(JSON.stringify({
    type: "pointer",
    x: e.offsetX / canvas.width,
    y: e.offsetY / canvas.height,
    buttons: e.buttons
  }));
});
```

---

## 🧠 Summary of Challenges

| Area | Challenges | Solutions |
|------|------------|-----------|
| Multi-user isolation | Each user must run isolated compositor + session | systemd user services; separate Wayland sockets |
| Permission model | Portal may pop UI prompt | Pre-authorize or custom portal backend |
| Encoding | HEVC browser support varies | WebCodecs negotiation; fallback to H.264 |
| Latency | WS backpressure & decode overload | Drop frames when decodeQueue grows |
| GPU limits | NVENC/VAAPI stream caps | Fallback to software encoding |
| Input injection | Use remote-desktop portal | JSON commands → Wayland events |

---

## 🔧 Suggested Repo Structure

```
repo/
  README.md
  gateway/
    src/
  agent/
    src/
  client/
    src/
  systemd/
    wayrd-session.service
    wayrd-agent.service
  scripts/
    wayrd-session-start.sh
```

---

## 🚀 Next Steps (for Codex)

**Stage 1:** Scaffold repository + modules  
**Stage 2:** Implement `/api/login` with PAM  
**Stage 3:** Create session manager (systemd integration)  
**Stage 4:** Implement PipeWire capture agent  
**Stage 5:** Implement WebSocket gateway  
**Stage 6:** Implement TS browser client (WebCodecs)  
**Stage 7:** Full integration & testing

