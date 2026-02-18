#!/usr/bin/env node
/**
 * Step 3 server: launches headless sway, captures periodic screenshots,
 * and serves them via WebSockets. Also accepts input events.
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

const WAYLAND_SOCKET = process.env.WAYLAND_SOCKET || 'remote-desktop';
const PORT = parseInt(process.env.PORT || '8080', 10);
const FPS = parseInt(process.env.FPS || '30', 10); // Higher FPS for video
const SCREENSHOT_INTERVAL_MS = 1000 / FPS;
console.log(`Screenshot interval: ${SCREENSHOT_INTERVAL_MS}ms`);

const VIDEO_CODEC = process.env.VIDEO_CODEC || 'h264';
const FFMPEG_PATH = path.join(PROJECT_ROOT, 'bin/ffmpeg');

const XDG_RUNTIME_DIR = fs.mkdtempSync(path.join(os.tmpdir(), 'remote-desktop-'));
fs.chmodSync(XDG_RUNTIME_DIR, 0o700);

const REQUIRED_BINARIES = ['sway', 'grim', 'wtype'];
const cleanupTasks: (() => void)[] = [
    () => {
        try {
            fs.rmSync(XDG_RUNTIME_DIR, { recursive: true, force: true });
        } catch (_) {
            // ignore
        }
    },
];

let compositorProcess: ChildProcess | undefined;



function ensureBinaries() {
    for (const binary of REQUIRED_BINARIES) {
        const result = spawnSync('which', [binary], { stdio: 'ignore' });
        if (result.status !== 0) {
            throw new Error(
                `Missing dependency "${binary}". Install it before running this script.`,
            );
        }
    }
    if (!fs.existsSync(FFMPEG_PATH)) {
        throw new Error(`Missing ffmpeg binary at ${FFMPEG_PATH}`);
    }
}

function wait(ms: number) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

async function startSway() {
    const swayConfigPath = path.join(XDG_RUNTIME_DIR, 'sway.conf');
    const swaySocketPath = path.join(XDG_RUNTIME_DIR, 'sway-ipc.sock');

    // Set SWAYSOCK env var so sway uses it
    process.env.SWAYSOCK = swaySocketPath;

    const configContent = `
# Minimal Sway Config for Headless Remote Desktop
input * {
    xkb_layout "us"
    xkb_model "pc105"
}
output HEADLESS-1 resolution 1280x720

# Enable XWayland
xwayland enable
`;
    fs.writeFileSync(swayConfigPath, configContent);

    const env = {
        ...process.env,
        WAYLAND_DISPLAY: '',
        XDG_RUNTIME_DIR,
        SWAYSOCK: swaySocketPath, // Explicitly set socket path
        WLR_BACKENDS: 'headless',
        WLR_LIBINPUT_NO_DEVICES: '1',
        WLR_RENDERER: 'pixman',
        LIBGL_ALWAYS_SOFTWARE: '1', // Suppress EGL/MESA warnings
        GSK_RENDERER: 'cairo', // Force GTK4 to use software rendering
        WLR_NO_HARDWARE_CURSORS: '1',
        GDK_BACKEND: 'wayland,x11', // Prefer Wayland, fallback to X11
        QT_QPA_PLATFORM: 'wayland;xcb', // Prefer Wayland, fallback to XCB
        CLUTTER_BACKEND: 'wayland',
        SDL_VIDEODRIVER: 'wayland',
    };
    // Cleanup stale X11 sockets/locks to prevent "No display available" error
    try {
        const x11Dir = '/tmp/.X11-unix';
        if (fs.existsSync(x11Dir)) {
            // In a real multi-user system this is dangerous, but for this dev env it's necessary
            // to recover from crashes where Xwayland didn't clean up.
            // We only remove sockets if we can't connect to them? Or just force remove?
            // Given the "fully broke" state, force cleanup is appropriate for the "server" startup.
            console.log('Cleaning up potential stale X11 sockets...');
            spawnSync('find', ['/tmp/.X11-unix', '-name', 'X*', '-delete']);
            spawnSync('find', ['/tmp', '-maxdepth', '1', '-name', '.X*-lock', '-delete']);
        }
    } catch (e) {
        console.warn('Failed to clean up X11 keys:', e);
    }

    // Using --debug for more info, can be removed if too noisy
    compositorProcess = spawn('sway', ['-c', swayConfigPath], { env, stdio: 'inherit' });
    cleanupTasks.push(() => {
        if (compositorProcess && !compositorProcess.killed) {
            compositorProcess.kill('SIGTERM');
        }
    });
    process.on('SIGINT', shutdown);
    process.on('SIGTERM', shutdown);

    // We can now wait for the specific socket file we defined
    await waitForSocketOrExit();
    console.log(`Headless sway ready (XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR}).`);

    // Check for XWayland socket
    setTimeout(() => {
        try {
            const x11Socket = fs.readdirSync('/tmp/.X11-unix').find(f => f.startsWith('X') && !f.includes('lock'));
            if (x11Socket) {
                console.log(`XWayland appears to be running on display :${x11Socket.replace('X', '')}`);
            } else {
                console.warn('XWayland socket not found in /tmp/.X11-unix. Legacy X11 apps may fail.');
            }
        } catch (e) {
            console.warn('Could not check XWayland status:', e);
        }
    }, 2000);
}

function waitForWaylandSocket(timeoutMs = 5000): Promise<string> {
    const start = Date.now();
    return new Promise((resolve, reject) => {
        (function poll() {
            try {
                // Check for Wayland socket (created by sway automatically, usually wayland-1)
                const entries = fs.readdirSync(XDG_RUNTIME_DIR);
                const socket = entries.find((entry) => entry.startsWith('wayland-') && !entry.endsWith('.lock'));
                if (socket) {
                    const target = path.join(XDG_RUNTIME_DIR, WAYLAND_SOCKET);
                    const source = path.join(XDG_RUNTIME_DIR, socket);
                    if (target !== source) {
                        try {
                            if (fs.existsSync(target)) fs.unlinkSync(target);
                            fs.symlinkSync(source, target);
                        } catch (_) {
                            // ignore
                        }
                    }
                    return resolve(socket);
                }
            } catch (_) {
                // directory may not be ready yet
            }
            if (Date.now() - start > timeoutMs) {
                return reject(
                    new Error(
                        `Timed out waiting for Wayland socket after ${timeoutMs}ms`,
                    ),
                );
            }
            setTimeout(poll, 100);
        })();
    });
}

function waitForSocketOrExit(): Promise<string> {
    return new Promise((resolve, reject) => {
        const onExit = (code: number | null) => {
            reject(new Error(`sway exited before exposing a socket (code ${code})`));
        };
        if (compositorProcess) compositorProcess.once('exit', onExit);
        waitForWaylandSocket()
            .then((socket) => {
                if (compositorProcess) compositorProcess.off('exit', onExit);
                resolve(socket);
            })
            .catch((err) => {
                if (compositorProcess) compositorProcess.off('exit', onExit);
                reject(err);
            });
    });
}

async function waitForXWayland(timeoutMs = 10000): Promise<string | undefined> {
    console.log('Waiting for XWayland to initialize...');
    const start = Date.now();
    while (Date.now() - start < timeoutMs) {
        try {
            const x11Socket = fs.readdirSync('/tmp/.X11-unix').find(f => f.startsWith('X') && !f.includes('lock'));
            if (x11Socket) {
                const display = ':' + x11Socket.replace('X', '');
                console.log(`XWayland ready on ${display}`);
                return display;
            }
        } catch (_) {
            // ignore
        }
        await wait(200);
    }
    console.warn('Timed out waiting for XWayland. X11 apps may fail to launch.');
    return undefined;
}

// --- Video Streaming Logic ---

let ffmpegProcess: ChildProcess | undefined;
cleanupTasks.push(() => {
    if (ffmpegProcess && !ffmpegProcess.killed) {
        ffmpegProcess.kill();
    }
    if (ffmpegProcess && !ffmpegProcess.killed) {
        ffmpegProcess.kill();
    }
});

function startStreaming(wss: WebSocketServer) {
    const codec = VIDEO_CODEC;
    const ffmpegCodec = codec === 'h265' ? 'libx265' : 'libx264';
    const ffmpegFormat = codec === 'h265' ? 'hevc' : 'h264';

    const ffmpegArgs = [
        '-f', 'image2pipe',
        '-vcodec', 'ppm',
        '-probesize', '32', // Low probe size for faster startup
        '-analyzeduration', '0', // No analyze duration
        '-fflags', 'nobuffer', // No input buffering
        '-r', `${FPS}`,
        '-i', '-',
        '-c:v', ffmpegCodec,
        '-pix_fmt', 'yuv420p',
        '-profile:v', 'baseline',
        '-level', '3.1',
        '-g', `${FPS}`, // GOP size equal to FPS (1 keyframe/sec)
        '-preset', 'ultrafast',
        '-tune', 'zerolatency',
        // Reduce buffering latency - using x264-params for fine tuning
        // slice-max-size: limits slice size for lower latency packetization
        // keyint/min-keyint: force keyframes (though we set -g already)
        // scenecut=0: disable scene detection for consistent latency
        // intra-refresh=1: use periodic intra refresh instead of IDR frames for smoother stream
        '-x264-params', 'slice-max-size=1200:keyint=60:min-keyint=60:scenecut=0:intra-refresh=1',
        '-f', ffmpegFormat,
        '-'
    ];

    console.log(`Starting ffmpeg with codec: ${ffmpegCodec} (grim input)`);
    ffmpegProcess = spawn(FFMPEG_PATH, ffmpegArgs);

    ffmpegProcess.stdout?.on('data', (data) => {
        wss.clients.forEach((client) => {
            if (client.readyState === WebSocket.OPEN) {
                client.send(data);
            }
        });
    });

    ffmpegProcess.stderr?.on('data', (data) => {
        // ffmpeg logs to stderr, can be noisy
        // console.error(`ffmpeg stderr: ${data}`);
    });

    ffmpegProcess.on('exit', (code) => {
        if (compositorProcess && !compositorProcess.killed) {
            console.error(`ffmpeg exited with code ${code}, restarting in 1s...`);
            setTimeout(() => startStreaming(wss), 1000);
        }
    });
}

async function captureLoop(wss: WebSocketServer) {
    console.log('Starting capture loop...');
    let loopCount = 0;
    while (true) {
        const loopStart = Date.now();
        loopCount++;
        const clientsCount = wss.clients.size;
        const ffmpegRunning = ffmpegProcess && !ffmpegProcess.killed;
        const stdinWritable = ffmpegProcess?.stdin?.writable;

        // Force capture every 5 seconds even without clients to debug grim
        const forceDebug = loopCount % (5000 / SCREENSHOT_INTERVAL_MS) === 0;

        if ((clientsCount > 0 || forceDebug) && ffmpegRunning && stdinWritable) {
            try {
                if (forceDebug) console.log(`Debug capture: clients=${clientsCount}`);

                const env = {
                    ...process.env,
                    WAYLAND_DISPLAY: WAYLAND_SOCKET,
                    XDG_RUNTIME_DIR,
                };
                const grim = spawn('grim', ['-t', 'ppm', '-'], { env });

                if (grim.stderr) {
                    grim.stderr.on('data', (data) => console.error(`grim stderr: ${data}`));
                }

                await new Promise((resolve) => {
                    if (ffmpegProcess?.stdin) {
                        grim.stdout.pipe(ffmpegProcess.stdin, { end: false });
                    } else {
                        console.error('ffmpeg stdin not available');
                    }

                    const timeout = setTimeout(() => {
                        console.error('grim timed out, killing...');
                        grim.kill();
                        resolve(null);
                    }, 2000); // Increased timeout to 2s

                    grim.on('exit', (code) => {
                        clearTimeout(timeout);
                        if (code !== 0) console.error(`grim exited with code ${code}`);
                        else if (forceDebug) console.log('Grim finished successfully');
                        resolve(code);
                    });
                    grim.on('error', (err) => {
                        clearTimeout(timeout);
                        console.error('grim error:', err);
                        resolve(null);
                    });
                });
            } catch (err) {
                console.error('Capture failed:', err);
            }
        } else if (clientsCount > 0) {
            console.warn('Capture skipped:', {
                clients: clientsCount,
                ffmpegRunning,
                stdinWritable
            });
        }

        const elapsed = Date.now() - loopStart;
        const delay = Math.max(0, SCREENSHOT_INTERVAL_MS - elapsed);
        await wait(delay);
    }
}

// --- Input Logic ---

let screenWidth = 1024;
let screenHeight = 768;
let screenOutput = 'HEADLESS-1';

let swayIpcSocket: string | undefined;

function findIpcSocket(): string | undefined {
    // First check if we defined it in the config
    const definedSocket = path.join(XDG_RUNTIME_DIR, 'sway-ipc.sock');
    if (fs.existsSync(definedSocket)) {
        return definedSocket;
    }

    try {
        const entries = fs.readdirSync(XDG_RUNTIME_DIR);
        const socket = entries.find((entry) => entry.startsWith('sway-ipc'));
        if (socket) {
            return path.join(XDG_RUNTIME_DIR, socket);
        }
    } catch (_) { }
    return undefined;
}

async function updateScreenResolution(retries = 20): Promise<void> {
    if (!swayIpcSocket) {
        swayIpcSocket = findIpcSocket();
        if (!swayIpcSocket) {
            if (retries > 0) {
                // console.log('Waiting for sway IPC socket...');
                await wait(500);
                return updateScreenResolution(retries - 1);
            }
            console.error('Could not find sway IPC socket');
            return;
        }
    }

    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
        SWAYSOCK: swayIpcSocket,
    };

    for (let i = 0; i < retries; i++) {
        try {
            const result = spawnSync('swaymsg', ['-t', 'get_outputs'], { env, stdio: ['ignore', 'pipe', 'pipe'] });
            if (result.status === 0) {
                const outputs = JSON.parse(result.stdout.toString());
                if (outputs.length > 0 && outputs[0].rect) {
                    screenWidth = outputs[0].rect.width;
                    screenHeight = outputs[0].rect.height;
                    screenOutput = outputs[0].name || screenOutput;
                    console.log(`Detected resolution: ${screenWidth}x${screenHeight} on ${screenOutput}`);
                    return;
                }
            }
            // Suppress error logs during startup retries unless it's the last attempt
            if (i === retries - 1) {
                console.error(`swaymsg failed with code ${result.status}: ${result.stderr.toString()}`);
            }
        } catch (err) {
            if (i === retries - 1) {
                console.error(`Attempt ${i + 1} to get resolution failed:`, err);
            }
        }
        await wait(500);
    }
    console.warn('Failed to detect resolution after retries, using defaults');
}

function mapWebKeyToWtype(key: string): string {
    switch (key) {
        case 'Enter': return 'Return';
        case 'Backspace': return 'BackSpace';
        case 'ArrowUp': return 'Up';
        case 'ArrowDown': return 'Down';
        case 'ArrowLeft': return 'Left';
        case 'ArrowRight': return 'Right';
        case 'Escape': return 'Escape';
        case ' ': return 'space';
        default: return key;
    }
}

let virtualPointerProcess: ChildProcess | undefined;

function startVirtualPointer() {
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
    };
    const scriptPath = path.join(PROJECT_ROOT, 'src/virtual_pointer.py');

    const launch = () => {
        console.log('Starting virtual pointer process...');
        virtualPointerProcess = spawn('python3', [scriptPath], { env });
        virtualPointerProcess.stderr?.on('data', (data) => {
            console.log(`virtual_pointer stderr: ${data}`);
        });
        virtualPointerProcess.on('exit', (code) => {
            if (code !== 0 && compositorProcess && !compositorProcess.killed) {
                console.error(`virtual_pointer exited with code ${code}, restarting in 1s...`);
                setTimeout(launch, 1000);
            }
        });
    };

    launch();

    cleanupTasks.push(() => {
        if (virtualPointerProcess && !virtualPointerProcess.killed) {
            virtualPointerProcess.kill();
        }
    });
}

function injectKey(key: string) {
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
    };
    const wtypeKey = mapWebKeyToWtype(key);
    const child = spawn('wtype', ['-k', wtypeKey], { env });
    child.on('exit', (code) => {
        if (code !== 0) console.error(`wtype exited with code ${code}`);
    });
}

function injectMouseMove(nx: number, ny: number) {
    if (!virtualPointerProcess || virtualPointerProcess.killed) return;
    const x = Math.round(nx * screenWidth);
    const y = Math.round(ny * screenHeight);
    virtualPointerProcess.stdin?.write(`m ${x} ${y} ${screenWidth} ${screenHeight}\n`);
}

function injectMouseButton(button: number, type: 'mousedown' | 'mouseup') {
    if (!virtualPointerProcess || virtualPointerProcess.killed) return;
    const state = type === 'mousedown' ? 1 : 0;
    virtualPointerProcess.stdin?.write(`b ${button} ${state}\n`);
}

// --- App Launching Logic ---
let x11Display: string | undefined;

function spawnApp(command: string) {
    if (!swayIpcSocket) swayIpcSocket = findIpcSocket();
    if (!swayIpcSocket) {
        console.error('Cannot spawn app: sway IPC socket not found');
        return;
    }

    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
        SWAYSOCK: swayIpcSocket,
    };

    let execCmd = command;
    if (x11Display && !process.env.DISPLAY) {
        execCmd = `env DISPLAY=${x11Display} ${command}`;
    }
    console.log(`Spawning app via swaymsg exec: ${execCmd}`);
    const child = spawn('swaymsg', ['exec', execCmd], { env, stdio: 'ignore' });
    child.unref();
}

// --- Main ---

async function main() {
    ensureBinaries();
    await startSway();

    // Get initial resolution
    await updateScreenResolution();

    startVirtualPointer();

    // Wait for XWayland to be ready before launching X11 apps
    x11Display = await waitForXWayland();

    // Start a simple demo app so we have something to see
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
    };

    // Launch initial app (calculator)
    spawnApp('gnome-calculator');

    // Start HTTP Server
    const server = await import('node:http').then(m => m.createServer((req, res) => {
        if (req.method === 'GET') {
            let urlPath = req.url || '/';
            const clientIp = req.socket.remoteAddress;
            console.log(`HTTP ${req.method} ${urlPath} from ${clientIp}`);

            if (urlPath === '/') urlPath = '/viewer.html';

            const filePath = path.join(__dirname, '../public', urlPath);

            // Prevent directory traversal
            if (!filePath.startsWith(path.join(__dirname, '../public'))) {
                res.writeHead(403);
                res.end('Forbidden');
                return;
            }

            if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
                const ext = path.extname(filePath);
                const contentType = ext === '.html' ? 'text/html' : ext === '.js' ? 'application/javascript' : 'text/plain';
                res.writeHead(200, { 'Content-Type': contentType });
                fs.createReadStream(filePath).pipe(res);
                return;
            }
        }
        res.writeHead(404);
        res.end('Not Found');
    }));

    const wss = new WebSocketServer({
        server,
        verifyClient: (info, done) => {
            console.log(`WebSocket Upgrade request from ${info.req.socket.remoteAddress}`);
            done(true);
        }
    });
    server.on('upgrade', (req, socket, head) => {
        console.log(`HTTP Upgrade event from ${req.socket.remoteAddress}`);
    });

    server.listen(PORT, '0.0.0.0', () => {
        console.log(`Server listening on http://0.0.0.0:${PORT}`);
    });

    wss.on('connection', (ws, req) => {
        const ip = req.socket.remoteAddress;
        console.log(`Client connected from ${ip}`);

        ws.on('error', (err) => {
            console.error('Client socket error:', err);
        });

        ws.on('close', (code, reason) => {
            console.log(`Client disconnected. Code: ${code}, Reason: ${reason}`);
        });

        ws.on('message', (data) => {
            try {
                const msg = JSON.parse(data.toString());

                if (msg.type === 'keydown' && msg.key) {
                    const mappedKey = mapWebKeyToWtype(msg.key);
                    // console.log(`Injecting key: ${msg.key} -> ${mappedKey}`);
                    injectKey(msg.key);
                } else if (msg.type === 'mousemove' && typeof msg.x === 'number' && typeof msg.y === 'number') {
                    // console.log(`Mouse move: ${msg.x}, ${msg.y}`);
                    injectMouseMove(msg.x, msg.y);
                } else if ((msg.type === 'mousedown' || msg.type === 'mouseup') && typeof msg.button === 'number') {
                    console.log(`Mouse ${msg.type === 'mousedown' ? 'down' : 'up'}: ${msg.button}`);
                    injectMouseButton(msg.button, msg.type);
                } else if (msg.type === 'ping' && typeof msg.timestamp === 'number') {
                    ws.send(JSON.stringify({ type: 'pong', timestamp: msg.timestamp }));
                } else if (msg.type === 'spawn' && msg.command) {
                    const allowed = ['gnome-calculator', 'weston-terminal', 'gedit', 'mousepad', 'xclock', 'xeyes'];
                    if (allowed.includes(msg.command)) {
                        spawnApp(msg.command);
                    } else {
                        console.warn(`Blocked spawn attempt for: ${msg.command}`);
                    }
                }

            } catch (err) {
                console.error('Failed to parse message:', err);
            }
        });
    });

    // Video Streaming
    startStreaming(wss);
    captureLoop(wss);
}

function shutdown() {
    console.log('Shutting down...');
    while (cleanupTasks.length) {
        const fn = cleanupTasks.pop();
        try {
            if (fn) fn();
        } catch (err) {
            console.warn('Cleanup step failed:', err);
        }
    }
    process.exit(0);
}

main().catch((err) => {
    console.error(err);
    shutdown();
});
