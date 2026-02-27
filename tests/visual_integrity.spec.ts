import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';

let serverProcess: ChildProcess;
const PORT = 3000 + Math.floor(Math.random() * 1000);

test.beforeAll(async () => {
    console.log(`Starting server on port ${PORT}...`);
    serverProcess = spawn('npm', ['start'], {
        env: { ...process.env, PORT: PORT.toString(), DISPLAY_NUM: (PORT + 100).toString(), FPS: '30', RTP_PORT: (PORT + 1000).toString() },
        stdio: 'pipe'
    });

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server Error]: ${data}`));

    // Wait for server to be ready
    await new Promise<void>((resolve) => {
        serverProcess.stdout?.on('data', (data) => {
            if (data.toString().includes('Server is ready')) resolve();
        });
        setTimeout(resolve, 10000);
    });

    // Kill background processes that cause noise/EMFILE
    // Kill background processes that cause noise/EMFILE
    try {
        const killCmd = "pkill -f 'xfdesktop|tracker|tumblerd|xfce4-panel|gvfsd'";
        require('child_process').execSync(killCmd, { stdio: 'ignore' });
        console.log('Killed background processes.');
    } catch (e) {
        // ignore if processes not found
    }

    // Set background to RED using xsetroot if available
    try {
        const xsetroot = spawn('xsetroot', ['-solid', '#b00000', '-display', ':101']); // Darker red to be safe
        xsetroot.on('error', (err) => console.log('xsetroot failed:', err));
    } catch (e) {
        console.log('xsetroot not found, skipping background set');
    }
});

test.afterAll(() => {
    if (serverProcess) {
        serverProcess.kill();
    }
    spawn('pkill', ['-f', 'Xvfb']);
    spawn('pkill', ['-f', 'ffmpeg']);
});

test('verify video integrity (no green artifacts)', async ({ page }) => {
    test.setTimeout(120000); // Allow time for retries

    console.log('Navigating to viewer...');
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
    await page.goto(`http://localhost:${PORT}/viewer.html`);

    // Retry loop for connection and stats
    console.log('Waiting for connection and stats...');
    await expect(async () => {
        // Move the mouse to keep video updating
        await page.mouse.move(200 + Math.random() * 400, 150 + Math.random() * 300);
        await page.waitForTimeout(100);

        const status = await page.locator('#status').textContent();
        // console.log(`Current status: ${status}`);
        expect(status).toMatch(/Connected|FPS/);

        // Verify we're actually rendering frames (dimensions become available).
        const dims = await page.evaluate(() => {
            const v = document.getElementById('webrtc-video') as HTMLVideoElement | null;
            const c = document.getElementById('display') as HTMLCanvasElement | null;
            return {
                videoW: v?.videoWidth || 0,
                videoH: v?.videoHeight || 0,
                canvasW: c?.width || 0,
                canvasH: c?.height || 0,
            };
        });
        console.log('Render dims:', dims);
        expect(dims.videoW).toBeGreaterThan(0);
        expect(dims.videoH).toBeGreaterThan(0);
        expect(dims.canvasW).toBeGreaterThan(0);
        expect(dims.canvasH).toBeGreaterThan(0);
    }).toPass({ timeout: 30000 });
    console.log('Connection and stats verified.');

    // Capture Server-Side Screenshot to debug Xvfb/Green Screen
    console.log('Capturing server-side screenshot...');
    const ffmpegBin = path.resolve('bin/ffmpeg');
    const screenshotProc = spawn(ffmpegBin, [
        '-y',
        '-f', 'x11grab',
        '-video_size', '1280x720',
        '-i', ':101',
        '-vframes', '1',
        'test-results/server_screenshot.png'
    ]);
    screenshotProc.stderr?.on('data', d => console.log(`[Screenshot]: ${d}`));
    await new Promise(r => screenshotProc.on('close', r));
    console.log('Server-side screenshot captured.');

    // Pixel Analysis Loop
    console.log('Starting pixel analysis...');
    await expect(async () => {
        const isGreenArtifact = await page.evaluate(() => {
            const canvas = document.getElementById('display') as HTMLCanvasElement;
            const ctx = canvas.getContext('2d');
            if (!ctx) return { greenCount: -1, redCount: -1 };

            const { width, height } = canvas;
            // Check grid of points
            let greenCount = 0;
            let redCount = 0;
            let pointsChecked = 0;

            for (let x = width * 0.25; x <= width * 0.75; x += width * 0.25) {
                for (let y = height * 0.25; y <= height * 0.75; y += height * 0.25) {
                    const pixel = ctx.getImageData(x, y, 1, 1).data;
                    const [r, g, b] = pixel;
                    pointsChecked++;

                    // Green Artifact: G is dominant, R and B are low (often 0,0,0 YUV -> RGB conversion artifact)
                    // or just G > R+B
                    if (g > r + 30 && g > b + 30) greenCount++;
                    // Red Background: R dominant
                    if (r > g + 30 && r > b + 30) redCount++;
                }
            }
            return { greenCount, redCount, pointsChecked };
        });

        console.log('Pixel Analysis:', isGreenArtifact);
        expect(isGreenArtifact.greenCount).toBe(0);
        expect(isGreenArtifact.pointsChecked).toBeGreaterThan(0);
        // We expect at least some red if xsetroot worked, but primarily NO green.
    }).toPass({ timeout: 10000 });
});
