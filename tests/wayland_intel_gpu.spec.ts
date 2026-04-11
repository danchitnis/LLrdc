import { test, expect } from '@playwright/test';
import { spawn, ChildProcess, execSync } from 'child_process';
import { waitForServerReady, waitForStreamingFrames } from './helpers';

const PORT = 8100 + Math.floor(Math.random() * 1000);

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Wayland Intel QSV GPU Acceleration', () => {
    const CONTAINER_NAME = `llrdc-test-wayland-intel-${PORT}`;

    test.beforeAll(async () => {
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
        console.log(`Starting server with --intel on port ${PORT}...`);
        
        serverProcess = spawn('./docker-run.sh', ['--intel', '--host-net'], {
            env: { 
                ...process.env, 
                IMAGE_TAG: 'local-test',
                PORT: PORT.toString(), 
                HOST_PORT: PORT.toString(),
                CONTAINER_NAME: CONTAINER_NAME
            },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Intel GPU Server start timeout. Output:\n${outputBuffer}`));
            }, 60000);

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
        await waitForServerReady(SERVER_URL);
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

        // Wait a moment for WebRTC to stabilize after a potential restart
        await page.waitForTimeout(2000);

        // Generate activity
        for (let i = 0; i < 5; i++) {
            await page.mouse.move(100 + i * 50, 100 + i * 50);
            await page.waitForTimeout(100);
        }

        await waitForStreamingFrames(page, `Stream should remain active: ${message}`, 30000);
    };

    test('should handle Intel QSV codec changes', async ({ page }) => {
        test.setTimeout(120000);
        
        page.on('console', msg => {
            if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
        });

        await page.goto(SERVER_URL);
        await page.click('body');

        const status = page.locator('#status');
        
        // 1. Initial State: H.264 QSV (default when --intel is used)
        await expect(status).toContainText(/h264_qsv|h264/i, { timeout: 45000 });
        await verifyStreaming(page, 'Initial H.264 QSV');

        // 2. Change Codec: H.264 QSV -> AV1 QSV
        console.log('Transitioning to AV1 QSV via reload...');
        await page.evaluate(() => {
            const sel = document.getElementById('video-codec-select') as HTMLSelectElement;
            if (sel) {
                sel.value = 'av1_qsv';
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }
        });
        await page.waitForTimeout(2000);
        await page.reload();
        await page.click('body');

        await expect(status).toContainText(/av1_qsv|av1/i, { timeout: 45000 });
        await verifyStreaming(page, 'AV1 QSV after reload');

        // 3. Change Codec: AV1 QSV -> H.265 CPU fallback
        console.log('Transitioning to H.265 CPU via reload...');
        await page.evaluate(() => {
            const sel = document.getElementById('video-codec-select') as HTMLSelectElement;
            if (sel) {
                sel.value = 'h265';
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }
        });
        await page.waitForTimeout(2000);
        await page.reload();
        await page.click('body');

        await expect(status).toContainText(/\[h265\]/i, { timeout: 45000 });
        await verifyStreaming(page, 'H.265 CPU after reload');

        console.log('Intel QSV reconfiguration scenarios verified!');
    });
});
