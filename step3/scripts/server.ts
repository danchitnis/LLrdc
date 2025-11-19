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
const PROJECT_ROOT = path.resolve(__dirname, '../..');
const LOCAL_TMP_DIR =
    process.env.LOCAL_TMP_DIR || path.join(PROJECT_ROOT, '.temp');
fs.mkdirSync(LOCAL_TMP_DIR, { recursive: true });

const WAYLAND_SOCKET = process.env.WAYLAND_SOCKET || 'remote-desktop-3';
const PORT = parseInt(process.env.PORT || '8080', 10);
const FPS = parseInt(process.env.FPS || '2', 10); // Low FPS for grim
const SCREENSHOT_INTERVAL_MS = 1000 / FPS;

const XDG_RUNTIME_DIR =
    process.env.XDG_RUNTIME_DIR ||
    fs.mkdtempSync(path.join(os.tmpdir(), 'remote-desktop-step3-'));
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
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: '',
        XDG_RUNTIME_DIR,
        WLR_BACKENDS: 'headless',
        WLR_LIBINPUT_NO_DEVICES: '1',
        WLR_RENDERER: 'pixman',
    };
    // Using --debug for more info, can be removed if too noisy
    compositorProcess = spawn('sway', [], { env, stdio: 'inherit' });
    cleanupTasks.push(() => {
        if (compositorProcess && !compositorProcess.killed) {
            compositorProcess.kill('SIGTERM');
        }
    });
    process.on('SIGINT', shutdown);
    process.on('SIGTERM', shutdown);
    await waitForSocketOrExit();
    console.log(`Headless sway ready (XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR}).`);
}

function waitForWaylandSocket(timeoutMs = 5000): Promise<string> {
    const start = Date.now();
    return new Promise((resolve, reject) => {
        (function poll() {
            try {
                const entries = fs.readdirSync(XDG_RUNTIME_DIR);
                const socket = entries.find((entry) => entry.startsWith('wayland-'));
                if (socket) {
                    const target = path.join(XDG_RUNTIME_DIR, WAYLAND_SOCKET);
                    const source = path.join(XDG_RUNTIME_DIR, socket);
                    if (target !== source) {
                        try {
                            fs.rmSync(target);
                        } catch (_) {
                            // ignore
                        }
                        fs.symlinkSync(source, target);
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

function injectKey(key: string) {
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
    };
    const wtypeKey = mapWebKeyToWtype(key);
    // wtype -k <key>
    spawn('wtype', ['-k', wtypeKey], { env, stdio: 'ignore' });
}

// --- Main ---

async function main() {
    ensureBinaries();
    await startSway();

    // Start a simple demo app so we have something to see
    const env = {
        ...process.env,
        WAYLAND_DISPLAY: WAYLAND_SOCKET,
        XDG_RUNTIME_DIR,
    };
    console.log('Launching weston-terminal...');
    const app = spawn('weston-terminal', [], { env, stdio: 'ignore' });
    cleanupTasks.push(() => {
        if (app && !app.killed) app.kill();
    });

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
    server.listen(PORT, () => {
        console.log(`Server listening on http://localhost:${PORT}`);
    });

    wss.on('connection', (ws) => {
        console.log('Client connected');

        ws.on('message', (data) => {
            try {
                const msg = JSON.parse(data.toString());
                if (msg.type === 'keydown' && msg.key) {
                    const mappedKey = mapWebKeyToWtype(msg.key);
                    console.log(`Injecting key: ${msg.key} -> ${mappedKey}`);
                    injectKey(msg.key);
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
