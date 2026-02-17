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
const FPS = parseInt(process.env.FPS || '2', 10); // Low FPS for grim
const SCREENSHOT_INTERVAL_MS = 1000 / FPS;

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
    };
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

// --- Screenshot Logic ---

async function captureScreenshotBuffer(): Promise<Buffer> {
    return new Promise<Buffer>((resolve, reject) => {
        const env = {
            ...process.env,
            WAYLAND_DISPLAY: WAYLAND_SOCKET,
            XDG_RUNTIME_DIR,
        };
        // grim - writes to stdout
        const grim = spawn('grim', ['-'], { env });

        const chunks: Buffer[] = [];
        grim.stdout.on('data', (chunk) => chunks.push(chunk));

        grim.on('exit', (code) => {
            if (code === 0) {
                resolve(Buffer.concat(chunks));
            } else {
                reject(new Error(`grim exited with code ${code}`));
            }
        });
        grim.on('error', reject);
    });
}

// --- Input Logic ---

let screenWidth = 1024;
let screenHeight = 768;

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
    } catch (_) {}
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
                    console.log(`Detected resolution: ${screenWidth}x${screenHeight}`);
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
    
    console.log(`Spawning app via swaymsg exec: ${command}`);
    // Use swaymsg exec to ensure the app gets the correct environment (DISPLAY, etc.)
    const child = spawn('swaymsg', ['exec', command], { env, stdio: 'ignore' });
    child.unref();
}

// --- Main ---

async function main() {
    ensureBinaries();
    await startSway();

    // Get initial resolution
    await updateScreenResolution();

    startVirtualPointer();

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
        if (req.method === 'GET' && (req.url === '/' || req.url === '/index.html')) {
            res.writeHead(200, { 'Content-Type': 'text/html' });
            fs.createReadStream(path.join(__dirname, '../public/viewer.html')).pipe(res);
        } else {
            res.writeHead(404);
            res.end('Not Found');
        }
    }));

    const wss = new WebSocketServer({ server });
    server.listen(PORT, '0.0.0.0', () => {
        console.log(`Server listening on http://0.0.0.0:${PORT}`);
    });

    wss.on('connection', (ws) => {
        console.log('Client connected');

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

        ws.on('close', () => console.log('Client disconnected'));
    });

    // Screenshot Loop
    console.log(`Starting screenshot loop at ${FPS} FPS...`);
    setInterval(async () => {
        if (wss.clients.size === 0) return; // Don't capture if no one is watching

        try {
            const buffer = await captureScreenshotBuffer();
            wss.clients.forEach((client) => {
                if (client.readyState === WebSocket.OPEN) {
                    client.send(buffer);
                }
            });
        } catch (err) {
            console.error('Screenshot failed:', err);
        }
    }, SCREENSHOT_INTERVAL_MS);
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
