import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`);
    } catch (e) {
        // ignore if no process found
    }
}
const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Bandwidth Configuration', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            env: { ...process.env, PORT: PORT.toString(), FPS: '5', DISPLAY_NUM: DISPLAY_NUM.toString() },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
            }, 15000);

            serverProcess.stdout?.on('data', (data) => {
                const output = data.toString();
                outputBuffer += output;
                if (output.includes(`Server listening on http://0.0.0.0:${PORT}`)) {
                    clearTimeout(timeout);
                    resolve();
                }
            });

            serverProcess.stderr?.on('data', (data) => {
                outputBuffer += data.toString();
            });

            serverProcess.on('exit', (code) => {
                clearTimeout(timeout);
                reject(new Error(`Server exited early with code ${code}. Output:\n${outputBuffer}`));
            });
        });
        console.log('Server started.');
    });

    test.afterAll(async () => {
        console.log('Stopping server...');
        if (serverProcess) {
            serverProcess.kill('SIGTERM');
            await new Promise<void>((resolve) => {
                const timeout = setTimeout(() => {
                    if (!serverProcess.killed) serverProcess.kill('SIGKILL');
                    resolve();
                }, 5000);
                serverProcess.on('exit', () => {
                    clearTimeout(timeout);
                    resolve();
                });
            });
        }
        killPort(PORT);
    });

    test('should adjust bandwidth and restart video stream', async ({ page }) => {
        test.setTimeout(30000);

        await test.step('Navigate to viewer and verify initial playback', async () => {
            await page.goto(SERVER_URL);
            await expect(page).toHaveTitle(/Remote Desktop/);

            // Verify that decoding is happening initally
            await expect.poll(async () => {
                return await page.evaluate(() => {
                    const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                    return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats ? window.getStats().totalDecoded : 0);
                });
            }, {
                message: 'Video should be decoding initial frames',
                timeout: 10000,
            }).toBeGreaterThan(0);
        });

        await test.step('Switch bandwidth to 1 Mbps', async () => {
            // Capture the frames we have decoded SO FAR. 
            const framesBeforeConfig = await page.evaluate(() => {
                const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats().totalDecoded || 0);
            });

            const content = await page.content();
            console.log("HTML:", content.substring(0, 1000));

            // Select 1 Mbps from the dropdown
            const configBtnLocator = page.locator('#config-btn');
            await configBtnLocator.click();

            const selectLocator = page.locator('#bandwidth-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('1');

            // Wait to ensure ffmpeg restart has propagated through Go to the browser decoding process
            await page.waitForTimeout(3000);

            // Check that the buffer started rising AGAIN after we presumably broke and restarted playback.
            await expect.poll(async () => {
                return await page.evaluate(() => {
                    const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                    return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats ? window.getStats().totalDecoded : 0);
                });
            }, {
                message: 'Video should have resumed decoding frames after 1 Mbps switch',
                timeout: 10000,
            }).toBeGreaterThan(framesBeforeConfig + 5);
        });

        await test.step('Switch bandwidth to 10 Mbps', async () => {
            const framesBeforeConfig2 = await page.evaluate(() => {
                const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats().totalDecoded || 0);
            });

            const selectLocator = page.locator('#bandwidth-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('10');

            await page.waitForTimeout(3000);

            await expect.poll(async () => {
                return await page.evaluate(() => {
                    const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                    return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats ? window.getStats().totalDecoded : 0);
                });
            }, {
                message: 'Video should have resumed decoding frames after 10 Mbps switch',
                timeout: 10000,
            }).toBeGreaterThan(framesBeforeConfig2 + 5);
        });

        // Assert Server Output reflects the bandwidth change config 
        expect(outputBuffer).toContain('Target bandwidth changed to 1 Mbps, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target bandwidth changed to 10 Mbps, restarting ffmpeg...');
    });
});
