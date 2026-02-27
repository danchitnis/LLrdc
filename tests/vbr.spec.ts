import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
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
    return await page.evaluate(async () => {
        const pc = (window as any).rtcPeer as RTCPeerConnection | undefined;
        if (!pc) return 0;
        const stats = await pc.getStats(null);
        let bytes = 0;
        stats.forEach((report: any) => {
            if (report.type === 'inbound-rtp') {
                const kind = report.kind ?? report.mediaType;
                if (kind === 'video' && typeof report.bytesReceived === 'number') {
                    bytes = report.bytesReceived;
                }
            }
        });
        return bytes;
    });
}

async function measureAverageInboundMbps(page: any, durationMs: number, tick?: () => Promise<void>): Promise<number> {
    const startBytes = await getInboundVideoBytes(page);
    const startTime = Date.now();
    const endTime = startTime + durationMs;

    while (Date.now() < endTime) {
        if (tick) await tick();
        await page.waitForTimeout(200);
    }

    const endBytes = await getInboundVideoBytes(page);
    const elapsedSec = (Date.now() - startTime) / 1000;
    const deltaBytes = Math.max(0, endBytes - startBytes);
    const mbps = (deltaBytes * 8) / elapsedSec / 1_000_000;
    return mbps;
}

test.beforeAll(async () => {
    serverPort = await getFreePort();
    serverUrl = `http://localhost:${serverPort}/viewer.html`;

    // Random-ish display number; must not collide between tests.
    const displayNum = 100 + Math.floor(Math.random() * 100);

    serverProcess = spawn('npm', ['start'], {
        env: {
            ...process.env,
            PORT: String(serverPort),
            FPS: '30',
            DISPLAY_NUM: String(displayNum),
            TEST_MINIMAL_X11: '1',
        },
        stdio: 'pipe',
        detached: false,
    });

    // Wait until server prints readiness line.
    await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('Timeout waiting for server start')), 30000);
        const onData = (data: Buffer) => {
            if (data.toString().includes('Server listening on')) {
                clearTimeout(timeout);
                resolve();
            }
        };
        serverProcess.stdout?.on('data', onData);
        serverProcess.stderr?.on('data', onData);
        serverProcess.on('exit', (code) => {
            clearTimeout(timeout);
            reject(new Error(`Server exited early with code ${code}`));
        });
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
    await page.goto(serverUrl);

    // Wait for WebRTC peer connection to exist.
    await expect
        .poll(async () => {
            return await page.evaluate(() => !!(window as any).rtcPeer);
        }, { timeout: 30000, message: 'Expected window.rtcPeer to be initialized' })
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

    // Measure "idle" bandwidth over a longer window.
    const idleAvg = await measureAverageInboundMbps(page, 20_000);
    console.log(`Idle average inbound bitrate (20s): ${idleAvg.toFixed(2)} Mbps`);

    const activeAvg = await measureAverageInboundMbps(page, 8_000, async () => {
        await page.mouse.move(200 + Math.random() * 400, 150 + Math.random() * 300);
    });
    console.log(`Active average inbound bitrate (8s): ${activeAvg.toFixed(2)} Mbps`);

    // Expectations:
    // - Idle should be very low (VBR savings when screen is stable).
    // - Active cursor motion should increase bandwidth.
    expect(idleAvg).toBeLessThan(0.25);
    expect(activeAvg).toBeGreaterThan(idleAvg + 0.05);
});
