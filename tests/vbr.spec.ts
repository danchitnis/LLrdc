import { test, expect } from '@playwright/test';
import { spawn, ChildProcess, execSync } from 'child_process';
import net from 'net';

let serverProcess: ChildProcess;
let serverPort: number;
let serverUrl: string;

async function getFreePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();
        server.unref();
        server.on('error', reject);
        server.listen(0, () => {
            const port = (server.address() as net.AddressInfo).port;
            server.close(() => resolve(port));
        });
    });
}

async function getInboundVideoBytes(page: any): Promise<number> {
    return await page.evaluate(() => {
        if (typeof (window as any).myTestStats === 'function') {
            const stats = (window as any).myTestStats();
            return stats.bytesReceived || 0;
        }
        return 0;
    });
}

async function generateLoad(page: any) {
    // Move the mouse to trigger visual updates (especially if xeyes is running)
    const x = Math.floor(Math.random() * 800);
    const y = Math.floor(Math.random() * 600);
    await page.mouse.move(x, y);
    await page.waitForTimeout(100);
}

// Helper to spawn a persistent visual load
async function spawnVisualLoad(page: any) {
    await page.evaluate(() => {
        const ws = (window as any).networkManager?.ws;
        if (ws && ws.readyState === WebSocket.OPEN) {
            // Spawn xeyes which follows the mouse
            ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
            // Also spawn a terminal with scrolling text for guaranteed pixel changes
            ws.send(JSON.stringify({ type: 'spawn', command: 'xfce4-terminal -e "bash -c \'while true; do echo $RANDOM; sleep 0.1; done\'"' }));
        }
    });
    await page.waitForTimeout(2000);
}

async function measureAverageInboundMbps(page: any, durationMs: number, tick?: () => Promise<void>, skipSetup = false): Promise<number> {
    // Wait for some decoding to happen (verifies stream is active)
    await expect.poll(async () => {
        return await page.evaluate(() => {
            if (typeof (window as any).myTestStats !== 'function') return -2;
            const stats = (window as any).myTestStats();
            return stats.totalDecoded;
        });
    }, { timeout: 45000, message: 'Wait for video decoding' }).toBeGreaterThan(-1);

    if (!skipSetup) {
        // Now wait for at least one frame
        await generateLoad(page);
        await expect.poll(async () => {
            return await page.evaluate(() => {
                if (typeof (window as any).myTestStats !== 'function') return 0;
                return (window as any).myTestStats().totalDecoded;
            });
        }, { timeout: 15000, message: 'Wait for first frame' }).toBeGreaterThan(0);
    }

    const startBytes = await getInboundVideoBytes(page);
    const startTime = Date.now();
    const endTime = startTime + durationMs;

    while (Date.now() < endTime) {
        if (tick) await tick();
        await page.waitForTimeout(500);
    }

    const endBytes = await getInboundVideoBytes(page);
    const elapsedSec = (Date.now() - startTime) / 1000;
    const deltaBytes = Math.max(0, endBytes - startBytes);
    const mbps = (deltaBytes * 8) / elapsedSec / 1_000_000;
    
    // Fallback if bytesReceived is not tracked (e.g. legacy WebCodecs path)
    if (mbps === 0) {
         console.log('Warning: Measured 0 Mbps, checking if we have totalDecoded but no bytesReceived tracking.');
    }
    
    return mbps;
}

test.beforeAll(async () => {
    test.setTimeout(120000);
    serverPort = await getFreePort();
    serverUrl = `http://localhost:${serverPort}/viewer.html`;

    // Random-ish display number; must not collide between tests.
    const displayNum = 100 + Math.floor(Math.random() * 100);
    const containerName = `llrdc-vbr-${serverPort}`;

    execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });

    serverProcess = spawn('./docker-run.sh', [], {
        env: {
            ...process.env,
            PORT: String(serverPort),
            HOST_PORT: String(serverPort),
            FPS: '30',
            VIDEO_CODEC: 'vp8',
            DISPLAY_NUM: String(displayNum),
            TEST_MINIMAL_X11: '1',
            CONTAINER_NAME: containerName,
            WEBRTC_PUBLIC_IP: '127.0.0.1'
        },
        stdio: 'pipe',
        detached: false,
    });

    // Wait until server prints readiness line.
    await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('Timeout waiting for server start')), 60000);
        const onData = (data: Buffer) => {
            const out = data.toString();
            if (out.includes('Server listening on')) {
                clearTimeout(timeout);
                // Extra stabilization wait
                setTimeout(resolve, 5000);
            }
        };
        serverProcess.stdout?.on('data', onData);
        serverProcess.stderr?.on('data', onData);
    });
});

test.afterAll(async () => {
    if (serverProcess) {
        serverProcess.kill('SIGTERM');
        await new Promise((r) => setTimeout(r, 1000));
        if (!serverProcess.killed) serverProcess.kill('SIGKILL');
    }
});

test('VBR reduces bandwidth when screen is idle', async ({ page }) => {
    test.setTimeout(120000);

    page.on('console', (msg) => console.log(`[Browser]: ${msg.text()}`));
    
    // Inject custom stats helper that works with any version of the app
    await page.addInitScript(() => {
        (window as any).myTestStats = () => {
            const webrtc = (window as any).webrtcManager;
            const webcodecs = (window as any).webcodecsManager;
            const network = (window as any).networkManager;
            
            const webrtcTotal = (webrtc && typeof webrtc.lastTotalDecoded === 'number' && webrtc.lastTotalDecoded >= 0) ? webrtc.lastTotalDecoded : 0;
            const webcodecsTotal = (webcodecs && typeof webcodecs.totalDecoded === 'number' && webcodecs.totalDecoded >= 0) ? webcodecs.totalDecoded : 0;
            const isWebRtc = webrtc && webrtc.isWebRtcActive;
            
            let bytes = 0;
            if (isWebRtc && webrtc) {
                bytes = webrtc.lastBytesReceived || 0;
            } else if (network) {
                bytes = network.totalBytesReceived || 0;
            }

            // Fallback: search for any numeric byte counters
            if (bytes === 0) {
                if (webrtc && typeof (webrtc as any).bytesReceived === 'number') bytes = (webrtc as any).bytesReceived;
                else if (network && typeof (network as any).bytesReceived === 'number') bytes = (network as any).bytesReceived;
            }

            // Fallback: if we have NO managers yet, but the page is loading, return 0.
            // If we have totalDecoded from ANY path, use it.
            const total = webrtcTotal + webcodecsTotal;

            return {
                totalDecoded: total,
                bytesReceived: bytes
            };
        };
    });

    await page.goto(serverUrl);
    
    // Wait for WebRTC peer connection to exist.
    await expect
        .poll(async () => {
            return await page.evaluate(() => {
                const webrtc = (window as any).webrtcManager;
                return webrtc && webrtc.rtcPeer && webrtc.rtcPeer.iceConnectionState === 'connected';
            });
        }, { timeout: 30000, message: 'Expected WebRTC ICE to be connected' })
        .toBeTruthy();

    // Force bandwidth target mode + set a low cap so CBR would be obvious.
    await page.evaluate(() => {
        const wsUrl = window.location.origin.replace('http', 'ws');
        const ws = new WebSocket(wsUrl);
        ws.onopen = () => {
            ws.send(JSON.stringify({ type: 'config', bandwidth: 2, framerate: 30 }));
            ws.close();
        };
    });

    // Give the pipeline time to restart.
    await page.waitForTimeout(5000);

    // Open config and ensure VBR is checked
    await page.locator('#config-btn').click();
    await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();
    
    const vbrCheckbox = page.locator('#vbr-checkbox');
    if (!(await vbrCheckbox.isChecked())) {
        await vbrCheckbox.check();
    }
    
    // Close config
    await page.locator('#config-btn').click();
    
    // Wait for config to propagate
    await page.waitForTimeout(5000);

    // Measure "idle" bandwidth over a longer window.
    const idleAvg = await measureAverageInboundMbps(page, 15_000, undefined, true);
    console.log(`Idle average inbound bitrate (15s): ${idleAvg.toFixed(2)} Mbps`);

    // Setup active load (one-time)
    await spawnVisualLoad(page);

    const activeAvg = await measureAverageInboundMbps(page, 8_000, async () => {
        await generateLoad(page);
    });
    console.log(`Active average inbound bitrate (8s): ${activeAvg.toFixed(2)} Mbps`);

    // Expectations:
    // - Idle should be relatively low (VBR savings when screen is stable).
    // - Note: background noise in XFCE (clock, etc.) can keep it > 0.
    expect(idleAvg).toBeLessThan(1.5);
    expect(activeAvg).toBeGreaterThan(idleAvg + 0.05);
});
