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
    // Move the mouse aggressively to trigger xeyes tracking and cursor updates
    for (let i = 0; i < 5; i++) {
        const x = Math.floor(Math.random() * 800);
        const y = Math.floor(Math.random() * 600);
        await page.mouse.move(x, y, { steps: 5 });
        await page.waitForTimeout(50);
    }
}

// Helper to spawn a persistent visual load
async function spawnVisualLoad(page: any) {
    await page.evaluate(() => {
        const nm = (window as any).networkManager;
        if (nm && typeof nm.sendMsg === 'function') {
            // Spawn xclock with 0.1 second update interval to ensure constant screen changes
            nm.sendMsg(JSON.stringify({ type: 'spawn', command: 'xclock -update 0.1 -geometry 400x400' }));
        }
    });
    // Give it more time to appear and start updating
    await page.waitForTimeout(5000);
}

async function measureAverageStats(page: any, durationMs: number, tick?: () => Promise<void>, skipSetup = false): Promise<{ mbps: number, fps: number }> {
    // Wait for some decoding to happen (verifies stream is active)
    await expect.poll(async () => {
        return await page.evaluate(() => {
            if (typeof (window as any).myTestStats !== 'function') return -2;
            const stats = (window as any).myTestStats();
            return stats.totalDecoded;
        });
    }, { timeout: 45000, message: 'Wait for video decoding' }).toBeGreaterThan(-1);

    if (!skipSetup) {
        const startFrames = await page.evaluate(() => (window as any).myTestStats().totalDecoded);
        // Now wait for at least some frames to be decoded during the load
        await generateLoad(page);
        await expect.poll(async () => {
            return await page.evaluate(() => {
                if (typeof (window as any).myTestStats !== 'function') return 0;
                return (window as any).myTestStats().totalDecoded;
            });
        }, { timeout: 15000, message: 'Wait for frames during load' }).toBeGreaterThan(startFrames);
    }

    const startBytes = await getInboundVideoBytes(page);
    const startTotalDecoded = await page.evaluate(() => (window as any).myTestStats().totalDecoded);
    const startTime = Date.now();
    const endTime = startTime + durationMs;

    while (Date.now() < endTime) {
        if (tick) await tick();
        await page.waitForTimeout(500);
    }

    const endBytes = await getInboundVideoBytes(page);
    const endTotalDecoded = await page.evaluate(() => (window as any).myTestStats().totalDecoded);
    const elapsedSec = (Date.now() - startTime) / 1000;
    
    const deltaBytes = Math.max(0, endBytes - startBytes);
    const mbps = (deltaBytes * 8) / elapsedSec / 1_000_000;
    
    const deltaFrames = Math.max(0, endTotalDecoded - startTotalDecoded);
    const fps = deltaFrames / elapsedSec;
    
    return { mbps, fps };
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

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data.toString().trim()}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server-Err]: ${data.toString().trim()}`));

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
            ws.send(JSON.stringify({ type: 'config', bandwidth: 2, framerate: 30, vbr: true, mpdecimate: true }));
            ws.close();
        };
    });

    // Give the pipeline time to restart.
    await page.waitForTimeout(5000);

    // Wait for config to propagate and screen to settle
    await page.waitForTimeout(5000);

    // Measure "idle" bandwidth and FPS over a shorter window as requested.
    const idleStats = await measureAverageStats(page, 10_000, undefined, true);
    console.log(`Idle average inbound bitrate (10s): ${idleStats.mbps.toFixed(2)} Mbps | FPS: ${idleStats.fps.toFixed(2)}`);

    // Setup active load (one-time)
    await spawnVisualLoad(page);

    const activeStats = await measureAverageStats(page, 15_000, async () => {
        await generateLoad(page);
    });
    console.log(`Active average inbound bitrate (15s): ${activeStats.mbps.toFixed(2)} Mbps | FPS: ${activeStats.fps.toFixed(2)}`);

    // Expectations:
    // - Idle should be very low (mpdecimate + VBR)
    expect(idleStats.mbps).toBeLessThan(0.5);
    // - Active should be higher than idle
    expect(activeStats.mbps).toBeGreaterThan(idleStats.mbps + 0.005);
    // - Active FPS should be noticeably higher than idle FPS
    expect(activeStats.fps).toBeGreaterThan(idleStats.fps + 2);
});
