import { test, expect } from '@playwright/test';
import { spawn, ChildProcess, execSync } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 150 + Math.floor(Math.random() * 50);

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Wayland GPU Acceleration and Reconfiguration', () => {
    const CONTAINER_NAME = `llrdc-test-wayland-gpu-${PORT}`;

    test.beforeAll(async () => {
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
        console.log(`Starting server with --wayland and --gpu on port ${PORT}...`);
        
        serverProcess = spawn('./docker-run.sh', ['--wayland', '--gpu'], {
            env: { 
                ...process.env, 
                PORT: PORT.toString(), 
                HOST_PORT: PORT.toString(),
                DISPLAY_NUM: DISPLAY_NUM.toString(),
                CONTAINER_NAME: CONTAINER_NAME
            },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Wayland GPU Server start timeout. Output:\n${outputBuffer}`));
            }, 45000);

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
                if (data.toString().includes(`Server listening on http://0.0.0.0:${PORT}`)) {
                    clearTimeout(timeout);
                    resolve();
                }
            });

            serverProcess.on('exit', (code) => {
                clearTimeout(timeout);
                if (code !== 0 && !outputBuffer.includes('Server listening')) {
                   reject(new Error(`Server exited with code ${code}. Output:\n${outputBuffer}`));
                }
            });
        });

        await new Promise(r => setTimeout(r, 5000));
    });

    test.afterAll(async () => {
        if (serverProcess) {
            serverProcess.kill('SIGTERM');
        }
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch(e) {}
    });

    const verifyStreaming = async (page: any, message: string) => {
        console.log(`Verifying: ${message}`);

        await page.waitForFunction(() => {
            const statusEl = document.getElementById('status');
            return statusEl && statusEl.textContent && statusEl.textContent.includes('WebRTC');
        }, { timeout: 30000 });

        const getFrames = () => page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
        
        const f1 = await getFrames();
        await page.waitForTimeout(4000);
        const f2 = await getFrames();
        
        expect(f2, `Stream should be active: ${message}`).toBeGreaterThan(f1);
        console.log(`Active: ${f1} -> ${f2}`);
    };

    test('should handle FPS and codec changes without freezing', async ({ page }) => {
        test.setTimeout(50000); // 50 seconds
        
        page.on('console', msg => {
            if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
        });

        await page.goto(SERVER_URL);
        await page.click('body');

        const status = page.locator('#status');
        
        // 1. Initial State: H.264 @ 30 FPS
        await expect(status).toContainText(/h264_nvenc|h264/i, { timeout: 20000 });
        await verifyStreaming(page, 'Initial H.264 @ 30 FPS');

        // 2. Change FPS: 30 -> 60
        console.log('Changing FPS to 60...');
        await page.click('#config-btn');
        await page.selectOption('#framerate-select', '60');
        
        // Wait for config broadcast
        await page.waitForFunction(() => {
            const statusEl = document.getElementById('status');
            return statusEl && statusEl.textContent && statusEl.textContent.includes('FPS: 60');
        }, { timeout: 15000 });

        await page.waitForTimeout(1000);
        await verifyStreaming(page, 'H.264 @ 60 FPS');

        // 3. Change Codec: H.264 -> AV1
        console.log('Transitioning to AV1 NVENC...');
        const av1LogPromise = page.waitForEvent('console', msg => msg.text().includes('Server codec: av1_nvenc'));
        await page.selectOption('#video-codec-select', 'av1_nvenc');
        await av1LogPromise;

        await expect(status).toContainText(/av1_nvenc|av1/i, { timeout: 20000 });
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'AV1 NVENC after transition');

        console.log('All reconfiguration scenarios verified!');
    });
});
