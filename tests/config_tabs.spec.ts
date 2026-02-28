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

test.describe('Tabular Configuration', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            env: { ...process.env, PORT: PORT.toString(), FPS: '5', DISPLAY_NUM: DISPLAY_NUM.toString() },
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
        killPort(PORT);
    });

    test('should adjust cpu and mouse options and restart video stream', async ({ page }) => {
        test.setTimeout(45000);

        await test.step('Navigate to viewer and verify initial playback', async () => {
            await page.goto(SERVER_URL);
            await expect(page).toHaveTitle(/LLrdc/);

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

        await test.step('Switch to CPU tab and adjust effort to 8', async () => {
            const configBtnLocator = page.locator('#config-btn');
            await configBtnLocator.click();

            const cpuTabLocator = page.locator('.config-tab-btn[data-tab="tab-cpu"]');
            await cpuTabLocator.click();

            const cpuEffortLocator = page.locator('#cpu-effort-slider');
            await cpuEffortLocator.waitFor({ state: 'visible', timeout: 10000 });
            
            // Set slider value and dispatch change event to trigger sendConfig
            await cpuEffortLocator.evaluate((el: HTMLInputElement) => {
                el.value = '8';
                el.dispatchEvent(new Event('change', { bubbles: true }));
            });

            await page.waitForTimeout(3000);
        });

        await test.step('Adjust CPU threads to 8', async () => {
            const cpuThreadsLocator = page.locator('#cpu-threads-select');
            await cpuThreadsLocator.waitFor({ state: 'visible', timeout: 10000 });
            await cpuThreadsLocator.selectOption('8');

            await page.waitForTimeout(3000);
        });

        await test.step('Switch to Mouse tab and disable desktop mouse', async () => {
            const mouseTabLocator = page.locator('.config-tab-btn[data-tab="tab-mouse"]');
            await mouseTabLocator.click();

            const mouseCheckboxLocator = page.locator('#desktop-mouse-checkbox');
            await mouseCheckboxLocator.waitFor({ state: 'visible', timeout: 10000 });
            await mouseCheckboxLocator.uncheck();

            await page.waitForTimeout(3000);
        });

        // Assert Server Output reflects the config changes
        expect(outputBuffer).toContain('Target CPU effort changed to 8, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target CPU threads changed to 8, restarting ffmpeg...');
        expect(outputBuffer).toContain('Target draw mouse changed to false, restarting ffmpeg...');
    });
});
