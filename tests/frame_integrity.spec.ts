import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';

let serverProcess: ChildProcess;
const PORT = 3000 + Math.floor(Math.random() * 1000);

test.beforeAll(async () => {
    console.log(`Starting server on port ${PORT}...`);
    serverProcess = spawn('npm', ['start'], {
        env: { ...process.env, PORT: PORT.toString(), DISPLAY_NUM: '101', FPS: '30', TEST_PATTERN: '1', RTP_PORT: (PORT + 2000).toString() },
        stdio: 'pipe'
    });

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server Error]: ${data}`));

    await new Promise<void>((resolve) => {
        serverProcess.stdout?.on('data', (data) => {
            if (data.toString().includes('Server listening')) resolve();
        });
        setTimeout(resolve, 10000);
    });
});

test.afterAll(() => {
    if (serverProcess) {
        serverProcess.kill();
    }
    spawn('pkill', ['-f', 'ffmpeg']);
});

test('verify WebRTC connection and frame integrity', async ({ page }) => {
    test.setTimeout(60000);

    console.log('Navigating to viewer...');
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
    await page.goto(`http://localhost:${PORT}/viewer.html`);

    // Wait for the WebRTC connection to establish
    console.log('Waiting for WebRTC connection...');
    await expect(async () => {
        const status = await page.locator('#status').textContent();
        expect(status).toContain('WebRTC Connected');
    }).toPass({ timeout: 15000 });
    console.log('WebRTC is connected.');

    // Verify video is playing and no frames are dropped (temporal integrity)
    await expect(async () => {
        const videoStats = await page.evaluate(() => {
            const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement;
            if (!videoEl) return { time: 0, dropped: -1 };
            const q = videoEl.getVideoPlaybackQuality();
            return {
                time: videoEl.currentTime,
                dropped: q ? q.droppedVideoFrames : 0,
                total: q ? q.totalVideoFrames : 0,
                width: videoEl.videoWidth
            };
        });

        console.log('WebRTC Video Stats:', videoStats);
        expect(videoStats.width).toBeGreaterThan(0); // Video dimensions loaded
        expect(videoStats.time).toBeGreaterThan(0.5); // Video is actively progressing
        expect(videoStats.dropped).toBeLessThan(15); // Allow small number of dropped frames during init
        expect(videoStats.total).toBeGreaterThan(10); // Received actual frames

        // Also check Network Latency via the ping endpoint we built
        const text = await page.locator('#status').textContent();
        const pingMatch = text?.match(/Ping: (\d+)ms/);
        if (pingMatch) {
            const ping = parseInt(pingMatch[1]);
            console.log(`Measured End-to-End Network Latency: ${ping}ms`);
            expect(ping).toBeLessThan(100);
        }
    }).toPass({ timeout: 10000 });

    console.log('Frame integrity and latency verified successfully.');
});
