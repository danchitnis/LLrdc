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
        execSync(`fuser -k ${port}/tcp 2>/dev/null`);
    } catch (e) {
        // ignore if no process found
    }
}
const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('VP8 CPU Threads Configuration', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        const CONTAINER_NAME = `llrdc-test-vp8-threads-${PORT}`;
        serverProcess = spawn('./docker-run.sh', ['--wayland'], {
            env: { 
                ...process.env, 
                PORT: PORT.toString(), 
                HOST_PORT: PORT.toString(),
                FPS: '15', 
                DISPLAY_NUM: DISPLAY_NUM.toString(), 
                VIDEO_CODEC: 'vp8',
                CONTAINER_NAME: CONTAINER_NAME
            },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:
${outputBuffer}`));
            }, 20000);

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

    test('should apply cpu threads to vp8 encoder', async ({ page }) => {
        test.setTimeout(60000);

        await page.goto(SERVER_URL);
        await expect(page).toHaveTitle(/LLrdc/);

        // Wait for initial video
        await expect.poll(async () => {
            return await page.evaluate(() => {
                const v = document.getElementById('webrtc-video') as HTMLVideoElement;
                return v && v.getVideoPlaybackQuality ? v.getVideoPlaybackQuality().totalVideoFrames : 0;
            });
        }, {
            message: 'Video should be decoding initial frames',
            timeout: 15000,
        }).toBeGreaterThan(0);

        console.log('Video started. Changing CPU threads...');

        // Open config menu
        const configBtnLocator = page.locator('#config-btn');
        await configBtnLocator.click();

        // Switch to Performance tab
        const performanceTabLocator = page.locator('.config-tab-btn[data-tab="tab-performance"]');
        await performanceTabLocator.click();

        // Adjust CPU threads to 2
        const cpuThreadsLocator = page.locator('#cpu-threads-select');
        await cpuThreadsLocator.waitFor({ state: 'visible', timeout: 10000 });
        
        console.log('Setting threads to 2...');
        await cpuThreadsLocator.selectOption('2');
        await page.waitForTimeout(4000);

        // Verify log for threads=2
        // It might not contain "Target CPU threads changed to..." because it's processed in a batch config update
        // but we can check the Starting wf-recorder capture logs
        expect(outputBuffer).toContain('threads=2');

        // Adjust CPU threads to 8
        console.log('Setting threads to 8...');
        await cpuThreadsLocator.selectOption('8');
        await page.waitForTimeout(4000);

        // Verify log for threads=8
        expect(outputBuffer).toContain('threads=8');
        
        console.log('Threads configuration verified in logs.');
    });
});
