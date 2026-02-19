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
import { WebSocketServer, WebSocket } from 'ws';

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

// --- NAL Splitter & Streaming Logic ---

/**
 * Split H.264 stream into NAL units and send them along with timestamps.
 * 
 * Simple state machine to accumulate bytes until start code is found.
 * NAL Format: [00 00 00 01] or [00 00 01] prefix.
 */
class NALSplitter {
    private buffer: Buffer;

    constructor(private onNAL: (nal: Buffer) => void) {
        this.buffer = Buffer.alloc(0);
    }

    feed(data: Buffer) {
        this.buffer = Buffer.concat([this.buffer, data]);
        // console.log(`Splitter feed: ${data.length} bytes, total buffer: ${this.buffer.length}`);

        let offset = 0;

        while (true) {
            // Find NAL start code (0x000001 or 0x00000001)
            // We search from offset
            const idx = this.buffer.indexOf(Buffer.from([0, 0, 1]), offset);

            if (idx === -1) {
                // No more start codes, keep remainder
                if (offset > 0) {
                    this.buffer = this.buffer.subarray(offset);
                }
                break;
            }

            // If we found a start code, checks if it is preceeded by 00 (making it 00 00 00 01) or not
            // The start code is technically the 00 00 01 sequence.
            // But we want to emit the NAL *before* this start code if we have processed one logic.

            // Wait, standard way:
            // 1. Find start code.
            // 2. If valid NAL exists before this start code, emit it.
            // 3. Mark this start code as beginning of next NAL.

            // To properly handle the first NAL, we assume the stream starts with a start code.
            // If offset is 0 and idx is 0 (or 1 for 00 00 00 01), we just setup the start pointer.

            // Refined Logic:
            // Iterate through buffer.
            // If we find 00 00 01 at 'idx'
            // The data *before* this (from 'startOfNAL') is the previous NAL.

            // Handling the 4-byte start code (00 00 00 01)
            // If idx > 0 and buffer[idx-1] == 0, then the start code is actually at idx-1.

            let startCodeLen = 3;
            let actualStart = idx;
            if (idx > 0 && this.buffer[idx - 1] === 0) {
                actualStart = idx - 1;
                startCodeLen = 4;
            }

            if (offset === 0 && actualStart === 0) {
                // This is the very first start code in the buffer (start of stream or segment)
                // Just skip it and set offset to data
                offset = actualStart + startCodeLen;
                continue;
            }

            // If we are here, we found a start code at `actualStart`.
            // The data from `0` to `actualStart` is a complete NAL (assuming buffer started with a NAL start)
            // But wait, if `offset` was moved, we need to capture from `0`? No, we consumed buffer.
            // We need to keep track of where the *current* NAL started.
            // Actually simpler:
            // We just keep splitting.
            // If we find a start code, everything before it is a NAL unit (if any).
            // Then we keep the start code and continue.

            // However, `indexOf` searches from `offset`. 
            // If we strip the buffer every time, we lose context.
            // Let's not strip until end.

            // Case 1: We found a start code at `actualStart`.
            // We emit `buffer.subarray(0, actualStart)` which is the previous NAL.
            // Then we shift buffer: `buffer = buffer.subarray(actualStart)`
            // BUT, we need to include the start code in the NEXT NAL?
            // Usually valid NALs in Annex B include the start code for decoders, OR we assume decoders handle it.
            // WebCodecs `VideoDecoder` handles Annex B (which includes start codes).

            // So:
            // 1. Buffer has data.
            // 2. Search for start code *after* the beginning (to find the *end* of current NAL).
            // We need to skip the start code at the beginning of buffer if it exists.

            // Let's assume buffer starts with a start code (from previous iteration logic).
            // We skip 3 or 4 bytes.
            // Search for next 00 00 1.

            let searchStart = 3; // minimum skip
            // Check if it starts with 00 00 00 01
            if (this.buffer.length >= 4 && this.buffer[0] === 0 && this.buffer[1] === 0 && this.buffer[2] === 0 && this.buffer[3] === 1) {
                searchStart = 4;
            } else if (this.buffer.length >= 3 && this.buffer[0] === 0 && this.buffer[1] === 0 && this.buffer[2] === 1) {
                searchStart = 3;
            } else {
                // Buffer doesn't start with start code? 
                // This might happen if partial NAL or garbage.
                // We'll search for *first* start code.
                const firstStart = this.buffer.indexOf(Buffer.from([0, 0, 1]));
                if (firstStart === -1) {
                    // No start code at all, keep buffering
                    break;
                }
                // Discard data before first start code
                let realStart = firstStart;
                if (firstStart > 0 && this.buffer[firstStart - 1] === 0) realStart = firstStart - 1;
                this.buffer = this.buffer.subarray(realStart);
                continue; // Restart loop with clean buffer start
            }

            const nextStart = this.buffer.indexOf(Buffer.from([0, 0, 1]), searchStart);

            if (nextStart === -1) {
                // No *next* start code found yet. 
                // We have a partial NAL in buffer (starts at 0).
                // Wait for more data.
                break;
            }

            // We found the start of the NEXT NAL.
            // So `0` to `nextStart` (exclusive of 00 00 1) ???
            // Wait, `indexOf` returns position of `00`.
            // If it is `00 00 00 01`, the `00` is at `nextStart`.
            // But we need to check if preceding byte is `00`.

            let splitPoint = nextStart;
            if (nextStart > 0 && this.buffer[nextStart - 1] === 0) {
                splitPoint = nextStart - 1;
            }

            const nalUnit = this.buffer.subarray(0, splitPoint);
            this.onNAL(nalUnit);

            // Remove processed NAL from buffer
            this.buffer = this.buffer.subarray(splitPoint);
            // Loop continues to process next NAL in buffer
        }
    }
}


function startStreaming(wss: WebSocketServer) {
    const codec = VIDEO_CODEC;
    const ffmpegCodec = codec === 'h265' ? 'libx265' : 'libx264';
    const ffmpegFormat = codec === 'h265' ? 'hevc' : 'h264'; // or rawvideo for pure? 'h264' is raw annex b

    // x11grab input options
    const inputArgs = [
        '-f', 'x11grab',
        '-draw_mouse', '1',
        '-r', `${FPS}`,
        '-s', '1280x720',
        '-i', `${DISPLAY}`,
    ];

    const outputArgs = [
        '-vf', 'scale=1280:720', // Ensure strict resolution to avoid artifacts
        '-c:v', ffmpegCodec,
        '-pix_fmt', 'yuv420p',
        '-profile:v', 'baseline',
        '-level', '3.1',
        '-bf', '0', // No B-frames for low latency
        '-preset', 'ultrafast',
        '-tune', 'zerolatency',
        '-b:v', '2000k', // Cap bitrate at 2Mbps to prevent network congestion/drops
        '-maxrate', '2000k',
        '-bufsize', '4000k', // Allow some buffering bursts but keep average low
        '-g', '5', // Keyframe every 5 frames for fast recovery
        '-keyint_min', '5',
        '-x264-params', 'rc-lookahead=0:sync-lookahead=0:scenecut=0',
        '-f', ffmpegFormat,
        '-'
    ];

    const ffmpegArgs = [
        '-probesize', '32',
        '-analyzeduration', '0',
        '-fflags', 'nobuffer',
        ...inputArgs,
        ...outputArgs
    ];

    console.log(`Starting ffmpeg capture from ${DISPLAY}...`);
    const env = { ...process.env, DISPLAY };

    ffmpegProcess = spawn(FFMPEG_PATH, ffmpegArgs, { env });

    const splitter = new NALSplitter((nal) => {
        // Broadcast NAL
        // Protocol: [1 byte Type][8 bytes Timestamp][NAL Data]
        // Type 1 = Video
        // console.log(`Emit NAL: Size=${nal.length}`); // Silenced per user request

        const timestamp = Date.now();
        const header = Buffer.alloc(9);
        header.writeUInt8(1, 0); // Video Type
        header.writeDoubleBE(timestamp, 1);

        const packet = Buffer.concat([header, nal]);

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

    await startX11();

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
                }
            } catch (e) {
                // ignore
            }
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
