#!/usr/bin/env node
/**
 * Step 3 server: launches headless XFCE via Xvfb, captures periodic screenshots/stream,
 * and accepts input events via WebSocket to xdotool.
 */
import { spawn, spawnSync, ChildProcess } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import dgram from 'node:dgram';
import { WebSocketServer, WebSocket } from 'ws';
import { RTCPeerConnection, RTCSessionDescription, MediaStreamTrack, RtpPacket } from 'werift';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = path.resolve(__dirname, '..');
const LOCAL_TMP_DIR =
    process.env.LOCAL_TMP_DIR || path.join(PROJECT_ROOT, '.temp');
fs.mkdirSync(LOCAL_TMP_DIR, { recursive: true });

const PORT = parseInt(process.env.PORT || '8080', 10);
const FPS = parseInt(process.env.FPS || '30', 10);
// X11 Display number
const DISPLAY_NUM = process.env.DISPLAY_NUM || '99';
const DISPLAY = `:${DISPLAY_NUM}`;

const VIDEO_CODEC = process.env.VIDEO_CODEC || 'h264';
const FFMPEG_PATH = path.join(PROJECT_ROOT, 'bin/ffmpeg');

const REQUIRED_BINARIES = ['Xvfb', 'xfce4-session', 'xdotool', 'ffmpeg'];
const cleanupTasks: (() => void)[] = [];

let xvfbProcess: ChildProcess | undefined;
let sessionProcess: ChildProcess | undefined;
let ffmpegProcess: ChildProcess | undefined;

// WebRTC Tracks
const webrtcTracks: Set<MediaStreamTrack> = new Set();
// UDP Server for RTP
const rtpPort = parseInt(process.env.RTP_PORT || '5000');
const udpServer = dgram.createSocket('udp4');
udpServer.on('message', (msg) => {
    try {
        const packet = RtpPacket.deSerialize(msg);
        for (const track of webrtcTracks) {
            track.writeRtp(packet);
        }
    } catch (e) {
        // ignore malformed
    }
});
udpServer.bind(rtpPort);

// --- Helpers ---

function ensureBinaries() {
    for (const binary of REQUIRED_BINARIES) {
        // For ffmpeg we might have a local binary, check that first if it's in the list
        if (binary === 'ffmpeg' && fs.existsSync(FFMPEG_PATH)) {
            continue;
        }

        const result = spawnSync('which', [binary], { stdio: 'ignore' });
        if (result.status !== 0) {
            throw new Error(
                `Missing dependency "${binary}". Install it before running this script.`,
            );
        }
    }
}

function wait(ms: number) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

// --- X11 / XFCE Startup ---

async function startX11() {
    console.log(`Starting Xvfb on ${DISPLAY}...`);

    // 1. Clean up any stale locks for this display
    if (fs.existsSync(`/tmp/.X${DISPLAY_NUM}-lock`)) {
        console.log(`Removing stale lock file /tmp/.X${DISPLAY_NUM}-lock`);
        fs.unlinkSync(`/tmp/.X${DISPLAY_NUM}-lock`);
    }
    const socketPath = `/tmp/.X11-unix/X${DISPLAY_NUM}`;
    if (fs.existsSync(socketPath)) {
        console.log(`Removing stale socket ${socketPath}`);
        fs.unlinkSync(socketPath);
    }

    // 2. Start Xvfb
    // -screen 0 1280x720x24 defines the resolution and depth
    xvfbProcess = spawn('Xvfb', [DISPLAY, '-screen', '0', '1280x720x24', '-nolisten', 'tcp'], {
        stdio: 'inherit'
    });

    cleanupTasks.push(() => {
        if (xvfbProcess && !xvfbProcess.killed) {
            console.log('Killing Xvfb...');
            xvfbProcess.kill();
        }
    });

    // Wait for X server to be ready
    await waitForXServer();
    console.log('Xvfb is ready.');

    // 3. Setup X11 environment (disable screensaver/dpms)
    console.log('Configuring X11 (disabling screensaver/dpms)...');
    spawnSync('xset', ['s', 'off'], { env: { ...process.env, DISPLAY } });
    spawnSync('xset', ['-dpms'], { env: { ...process.env, DISPLAY } });
    spawnSync('xset', ['s', 'noblank'], { env: { ...process.env, DISPLAY } });

    // 4. Start XFCE Session
    console.log('Starting xfce4-session...');
    const env = { ...process.env, DISPLAY };
    sessionProcess = spawn('dbus-run-session', ['xfce4-session'], { env, stdio: 'inherit' });

    cleanupTasks.push(() => {
        if (sessionProcess && !sessionProcess.killed) {
            console.log('Killing xfce4-session...');
            sessionProcess.kill();
        }
    });

    // Give the session a moment to initialize
    await wait(3000);
    console.log('XFCE session started (assumed).');

    // Disable screen blanking and compositing (again to be sure)
    try {
        console.log('Configuring X11 post-start...');
        const xenv = { ...process.env, DISPLAY };
        spawnSync('xset', ['s', 'off'], { env: xenv });
        spawnSync('xset', ['-dpms'], { env: xenv });
        spawnSync('xset', ['s', 'noblank'], { env: xenv });
        // Disable compositing
        spawnSync('xfconf-query', ['-c', 'xfwm4', '-p', '/general/use_compositing', '-s', 'false'], { env: xenv });
    } catch (e) {
        console.error('Failed to configure X settings:', e);
    }
}

async function waitForXServer(timeoutMs = 10000) {
    const start = Date.now();
    const socketPath = `/tmp/.X11-unix/X${DISPLAY_NUM}`;
    while (Date.now() - start < timeoutMs) {
        if (fs.existsSync(socketPath)) {
            return;
        }
        await wait(100);
    }
    throw new Error(`Timed out waiting for X server at ${DISPLAY}`);
}

// --- IVF Splitter & Streaming Logic (VP8) ---

/**
 * Split IVF stream into frames.
 * IVF Header: 32 bytes.
 * Frame Header: 12 bytes (4 byte size, 8 byte timestamp).
 */
class IVFSplitter {
    private buffer: Buffer;
    private headerParsed: boolean = false;

    constructor(private onFrame: (frame: Buffer) => void) {
        this.buffer = Buffer.alloc(0);
    }

    feed(data: Buffer) {
        this.buffer = Buffer.concat([this.buffer, data]);

        // 1. Parse File Header (32 bytes)
        if (!this.headerParsed) {
            if (this.buffer.length >= 32) {
                const signature = this.buffer.toString('utf8', 0, 4);
                if (signature !== 'DKIF') {
                    console.error(`Invalid IVF Signature: ${signature}`);
                }
                // Discard header
                this.buffer = this.buffer.subarray(32);
                this.headerParsed = true;
            } else {
                return; // Wait for more data
            }
        }

        // 2. Parse Frames
        while (true) {
            // Need at least 12 bytes for Frame Header
            if (this.buffer.length < 12) {
                break;
            }

            const frameSize = this.buffer.readUInt32LE(0);
            // Timestamp is 64-bit LE, but we ignore it for now (use wall clock)

            // Check if we have the full frame
            if (this.buffer.length < 12 + frameSize) {
                break;
            }

            // Extract Frame
            const frameData = this.buffer.subarray(12, 12 + frameSize);
            this.onFrame(frameData);

            // Advance buffer
            this.buffer = this.buffer.subarray(12 + frameSize);
        }
    }
}

function startStreaming(wss: WebSocketServer) {
    const inputArgs = process.env.TEST_PATTERN ?
        ['-re', '-f', 'lavfi', '-i', `testsrc=size=1280x720:rate=${FPS}`] :
        ['-f', 'x11grab', '-video_size', '1280x720', '-i', `:${DISPLAY_NUM}`];

    const outputArgs = [
        '-vf', `fps=${FPS},format=yuv420p`,
        '-c:v', 'libvpx',
        '-b:v', '2000k',
        '-g', '120',     // Keyframe every 4 seconds (30fps)
        '-qmin', '4',
        '-qmax', '50',
        '-speed', '8',   // Realtime 5-8. 8 is fastest.
        '-quality', 'realtime',
        '-map', '0:v',
        // Tee muxer: one to RTP payload_type 96 (UDP 5000), one to IVF (pipe:1)
        '-f', 'tee',
        `[f=rtp:payload_type=96]rtp://127.0.0.1:${rtpPort}?pkt_size=1200|[f=ivf]pipe:1`
    ];

    const ffmpegArgs = [
        '-probesize', '32',
        '-analyzeduration', '0',
        '-fflags', 'nobuffer',
        '-threads', '2', // VP8 supports threading
        ...inputArgs,
        ...outputArgs
    ];

    console.log(`Starting ffmpeg capture (VP8) from ${DISPLAY}...`);
    const env = { ...process.env, DISPLAY };

    ffmpegProcess = spawn(FFMPEG_PATH, ffmpegArgs, { env });

    // Ensure we kill ffmpeg on exit
    process.on('exit', () => {
        if (ffmpegProcess) {
            ffmpegProcess.kill();
        }
    });
    process.on('SIGINT', () => process.exit());
    process.on('SIGTERM', () => process.exit());

    let lastVideoSent = 0;
    const targetInterval = 1000 / FPS;

    const splitter = new IVFSplitter((frame) => {
        // Broadcast raw IVF frame over WebSocket (Fallback)
        const timestamp = Date.now();
        // Protocol: [1 byte Type(1)][8 bytes Timestamp][VP8 Frame]
        const header = Buffer.alloc(9);
        header.writeUInt8(1, 0); // Video Type
        header.writeDoubleBE(timestamp, 1);

        const packet = Buffer.concat([header, frame]);

        wss.clients.forEach((client) => {
            if (client.readyState === WebSocket.OPEN) {
                client.send(packet);
            }
        });
    });

    ffmpegProcess.stdout?.on('data', (data) => {
        splitter.feed(data);
    });

    ffmpegProcess.stderr?.on('data', (data) => {
        const str = data.toString();
        // Filter out verbose frame info
        if (!str.startsWith('frame=') && !str.includes('[x11grab')) {
            console.error(`ffmpeg stderr: ${str}`);
        }
    });

    ffmpegProcess.on('exit', (code) => {
        console.log(`ffmpeg exited with code ${code}`);
        if (xvfbProcess && !xvfbProcess.killed) {
            console.log('Restarting ffmpeg in 1s...');
            setTimeout(() => startStreaming(wss), 1000);
        }
    });

    cleanupTasks.push(() => {
        if (ffmpegProcess && !ffmpegProcess.killed) {
            console.log('Killing ffmpeg...');
            ffmpegProcess.kill();
        }
    });
}

// --- Input Logic (xdotool) ---

const screenWidth = 1280;
const screenHeight = 720; // 720 matches Xvfb

function injectKey(key: string, type: 'keydown' | 'keyup') {
    // Map browser keys to X11 keysyms
    const keyMap: Record<string, string> = {
        'Control': 'Control_L',
        'Shift': 'Shift_L',
        'Alt': 'Alt_L',
        'Meta': 'Super_L',
        'Enter': 'Return',
        'Backspace': 'BackSpace',
        'ArrowUp': 'Up',
        'ArrowDown': 'Down',
        'ArrowLeft': 'Left',
        'ArrowRight': 'Right',
        'Escape': 'Escape',
        'Tab': 'Tab',
        'Home': 'Home',
        'End': 'End',
        'PageUp': 'Page_Up',
        'PageDown': 'Page_Down',
        'Delete': 'Delete',
        'Insert': 'Insert',
        ' ': 'space'
    };

    const xKey = keyMap[key] || key;
    if (!/^[a-zA-Z0-9_\-]+$/.test(xKey) && !Object.values(keyMap).includes(xKey)) return;

    const mode = type === 'keydown' ? 'keydown' : 'keyup';
    spawn('xdotool', [mode, xKey], { env: { ...process.env, DISPLAY } });
}

function injectMouseMove(nx: number, ny: number) {
    const x = Math.round(nx * screenWidth);
    const y = Math.round(ny * screenHeight);
    spawn('xdotool', ['mousemove', x.toString(), y.toString()], { env: { ...process.env, DISPLAY } });
}

function injectMouseButton(button: number, type: 'mousedown' | 'mouseup') {
    let xbtn = 1;
    if (button === 0) xbtn = 1;
    else if (button === 1) xbtn = 2;
    else if (button === 2) xbtn = 3;

    spawn('xdotool', [type, xbtn.toString()], { env: { ...process.env, DISPLAY } });
}

// --- App Launching Logic ---

function spawnApp(command: string) {
    const env = { ...process.env, DISPLAY };
    console.log(`Spawning app: ${command}`);
    const child = spawn(command, [], { env, detached: true, stdio: ['ignore', 'ignore', 'pipe'] });
    child.unref();
}

// --- Main ---

async function main() {
    ensureBinaries();

    if (!process.env.TEST_PATTERN) {
        await startX11();
    } else {
        console.log('TEST_PATTERN mode: skipping X11 setup.');
    }

    // Start HTTP Server
    const server = await import('node:http').then(m => m.createServer((req, res) => {
        console.log(`HTTP ${req.method} ${req.url} from ${req.socket.remoteAddress}`);
        if (req.method === 'GET') {
            let urlPath = req.url || '/';
            if (urlPath === '/') urlPath = '/viewer.html';

            const filePath = path.join(__dirname, '../public', urlPath);
            if (!filePath.startsWith(path.join(__dirname, '../public'))) {
                res.writeHead(403);
                res.end('Forbidden');
                return;
            }

            if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
                const ext = path.extname(filePath);
                const contentType = ext === '.html' ? 'text/html' : ext === '.js' ? 'application/javascript' : 'text/plain';
                // Add Headers for SharedArrayBuffer / WebCodecs if needed (future proofing)
                res.writeHead(200, {
                    'Content-Type': contentType,
                    'Cross-Origin-Opener-Policy': 'same-origin',
                    'Cross-Origin-Embedder-Policy': 'require-corp'
                });
                fs.createReadStream(filePath).pipe(res);
                return;
            }
        }
        res.writeHead(404);
        res.end('Not Found');
    }));

    const wss = new WebSocketServer({
        server,
        perMessageDeflate: false // Important for low latency/overhead
    });

    server.listen(PORT, '0.0.0.0', () => {
        console.log(`Server listening on http://0.0.0.0:${PORT}`);
    });

    wss.on('connection', (ws, req) => {
        console.log(`Client connected from ${req.socket.remoteAddress}`);

        // Optimize TCP Socket
        // @ts-ignore - access internal socket
        const socket = req.socket;
        if (socket) {
            socket.setNoDelay(true); // Disable Nagle
            console.log('TCP_NODELAY set on client socket');
        }

        let peer: RTCPeerConnection | undefined;
        let track: MediaStreamTrack | undefined;

        ws.on('message', (data) => {
            try {
                const msg = JSON.parse(data.toString());
                if ((msg.type === 'keydown' || msg.type === 'keyup') && msg.key) {
                    injectKey(msg.key, msg.type as any);
                } else if (msg.type === 'mousemove' && typeof msg.x === 'number') {
                    injectMouseMove(msg.x, msg.y);
                } else if ((msg.type === 'mousedown' || msg.type === 'mouseup')) {
                    injectMouseButton(msg.button, msg.type);
                } else if (msg.type === 'spawn' && msg.command) {
                    const allowed = ['gnome-calculator', 'weston-terminal', 'gedit', 'mousepad', 'xclock', 'xeyes', 'xfce4-terminal'];
                    if (allowed.includes(msg.command)) {
                        spawnApp(msg.command);
                    }
                } else if (msg.type === 'ping') {
                    // Echo back for latency measurement
                    ws.send(JSON.stringify({ type: 'pong', timestamp: msg.timestamp }));
                } else if (msg.type === 'webrtc_offer' && msg.sdp) {
                    peer = new RTCPeerConnection({
                        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
                    });
                    track = new MediaStreamTrack({ kind: 'video' });
                    peer.addTrack(track);
                    webrtcTracks.add(track);

                    peer.onIceCandidate.subscribe((candidate) => {
                        if (candidate && ws.readyState === WebSocket.OPEN) {
                            ws.send(JSON.stringify({ type: 'webrtc_ice', candidate }));
                        }
                    });

                    peer.setRemoteDescription(new RTCSessionDescription(msg.sdp, 'offer'));
                    peer.createAnswer().then((answer) => {
                        peer!.setLocalDescription(answer).then(() => {
                            if (ws.readyState === WebSocket.OPEN) {
                                ws.send(JSON.stringify({ type: 'webrtc_answer', sdp: peer!.localDescription }));
                            }
                        });
                    });

                } else if (msg.type === 'webrtc_ice' && msg.candidate) {
                    if (peer) {
                        peer.addIceCandidate(msg.candidate).catch(e => console.error('Failed to add ICE', e));
                    }
                }
            } catch (e) {
                // ignore
            }
        });

        ws.on('close', () => {
            if (track) webrtcTracks.delete(track);
            if (peer) peer.close();
        });
    });

    startStreaming(wss);
}

function shutdown() {
    console.log('Shutting down...');
    while (cleanupTasks.length) {
        const fn = cleanupTasks.pop();
        try { if (fn) fn(); } catch (e) { console.error(e); }
    }
    process.exit(0);
}

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);

main().catch((err) => {
    console.error(err);
    shutdown();
});
