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

test.describe('Framerate Configuration', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            env: { ...process.env, PORT: PORT.toString(), FPS: '30', DISPLAY_NUM: DISPLAY_NUM.toString() },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:
${outputBuffer}`));
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
                reject(new Error(`Server exited early with code ${code}. Output:
${outputBuffer}`));
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
        try {
            const containerId = execSync(`docker ps -q --filter "ancestor=danchitnis/llrdc" --filter "publish=${PORT}"`).toString().trim();
            if (containerId) {
                console.log(`Killing docker container ${containerId}...`);
                execSync(`docker kill ${containerId}`);
            }
        } catch (e) {
            console.error('Failed to kill docker container:', e);
        }
        killPort(PORT);
    });

    test('should adjust framerate and restart video stream', async ({ page }) => {
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

        await test.step('Switch framerate to 15 FPS', async () => {
            const framesBeforeConfig = await page.evaluate(() => {
                const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : (window.getStats().totalDecoded || 0);
            });

            // Select 15 FPS from the dropdown
            const configBtnLocator = page.locator('#config-btn');
            await configBtnLocator.click();

            const selectLocator = page.locator('#framerate-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('15');

            await page.waitForTimeout(3000);

            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().fps : 0);
            }, {
                message: 'Video FPS should reach at least 10 after 15 FPS switch',
                timeout: 10000,
            }).toBeGreaterThan(10);
        });

        await test.step('Switch framerate to 60 FPS', async () => {
            const selectLocator = page.locator('#framerate-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('60');

            await page.waitForTimeout(3000);

            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().fps : 0);
            }, {
                message: 'Video FPS should reach at least 45 after 60 FPS switch',
                timeout: 10000,
            }).toBeGreaterThan(45);
        });

        await test.step('Switch framerate to 90 FPS', async () => {
            const selectLocator = page.locator('#framerate-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('90');

            await page.waitForTimeout(3000);

            // Spawn xeyes and move mouse to generate load
            await page.evaluate(() => {
                const ws = new WebSocket(window.location.href.replace('http', 'ws'));
                ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
            });
            await page.waitForTimeout(1000);

            const overlayBox = await page.locator('#input-overlay').boundingBox();
            if (overlayBox) {
                await page.mouse.move(overlayBox.x + 100, overlayBox.y + 100);
                await page.mouse.down();
                await page.mouse.move(overlayBox.x + 600, overlayBox.y + 500, { steps: 100 });
                await page.mouse.up();
                await page.mouse.move(overlayBox.x + 200, overlayBox.y + 200, { steps: 100 });
            }

            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().fps : 0);
            }, {
                message: 'Video FPS should reach at least 70 after 90 FPS switch',
                timeout: 10000,
            }).toBeGreaterThan(70);
        });

        await test.step('Switch framerate to 120 FPS', async () => {
            const selectLocator = page.locator('#framerate-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('120');

            await page.waitForTimeout(3000);

            // Spawn xeyes and move mouse to generate load
            await page.evaluate(() => {
                const ws = new WebSocket(window.location.href.replace('http', 'ws'));
                ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
            });
            await page.waitForTimeout(1000);

            const overlayBox = await page.locator('#input-overlay').boundingBox();
            if (overlayBox) {
                await page.mouse.move(overlayBox.x + 100, overlayBox.y + 100);
                await page.mouse.down();
                await page.mouse.move(overlayBox.x + 600, overlayBox.y + 500, { steps: 100 });
                await page.mouse.up();
                await page.mouse.move(overlayBox.x + 200, overlayBox.y + 200, { steps: 100 });
            }

            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().fps : 0);
            }, {
                message: 'Video FPS should reach at least 90 after 120 FPS switch',
                timeout: 10000,
            }).toBeGreaterThan(90);
        });

        // Assert Server Output reflects the framerate change config 
        expect(outputBuffer).toContain('Target framerate changed to 15 fps, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target framerate changed to 60 fps, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target framerate changed to 90 fps, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target framerate changed to 120 fps, restarting ffmpeg...');
        expect(outputBuffer).not.toContain('WARNING: webrtcFrameChan is full');
    });
});