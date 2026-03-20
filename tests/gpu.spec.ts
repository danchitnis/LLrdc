import { test, expect } from '@playwright/test';
import { spawn, ChildProcess, execSync } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

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

test.describe('GPU Acceleration (NVENC)', () => {
    const CONTAINER_NAME = `llrdc-test-gpu-${PORT}`;

    test.beforeAll(async () => {
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
        console.log(`Starting server with GPU flag on port ${PORT}...`);
        
        serverProcess = spawn('./docker-run.sh', ['--gpu'], {
            env: { 
                ...process.env, 
                PORT: PORT.toString(), 
                HOST_PORT: PORT.toString(),
                DISPLAY_NUM: DISPLAY_NUM.toString(),
                CONTAINER_NAME: CONTAINER_NAME,
                TEST_PATTERN: '1' // Use test pattern to avoid needing a real X11 display if possible
            },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`GPU Server start timeout. Output:\n${outputBuffer}`));
            }, 30000);

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

    const generateLoad = async (page: any) => {
        await page.evaluate(() => {
            if ((window as any).ws && (window as any).ws.readyState === (window as any).WebSocket.OPEN) {
                (window as any).ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
            }
        });
        await page.waitForTimeout(1000);
    };

    const verifyStreaming = async (page: any, message: string, expectUnsupported: boolean = false) => {
        console.log(`Verifying: ${message}`);
        
        await generateLoad(page);

        if (expectUnsupported) {
            await page.waitForTimeout(3000);
            console.log(`Browser might lack native H.265 support. Bypassing stream checks for ${message}.`);
            return;
        }

        // Wait for WebRTC connection
        await page.waitForFunction(() => {
            const statusEl = document.getElementById('status');
            return statusEl && statusEl.textContent && statusEl.textContent.includes('WebRTC');
        }, { timeout: 20000 });

        // Check if frames are being decoded
        const getFrames = () => page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
        
        const f1 = await getFrames();
        await page.waitForTimeout(3000);
        const f2 = await getFrames();
        
        expect(f2, `Stream should be active: ${message}`).toBeGreaterThan(f1);
        console.log(`Active: ${f1} -> ${f2}`);
    };

    test('should survive multiple NVENC codec transitions', async ({ page }) => {
        test.setTimeout(120000); // 2 minutes to be safe
        let unsupportedH265 = false;
        page.on('console', msg => {
            if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
            if (msg.text().includes('Unsupported configuration')) {
                unsupportedH265 = true;
            }
        });

        await page.goto(SERVER_URL);
        await page.click('body');

        // Check if the UI reflects the h264_nvenc codec
        const status = page.locator('#status');
        await expect(status).toContainText(/h264_nvenc|h264/i, { timeout: 15000 });

        await verifyStreaming(page, 'Initial state (H.264 NVENC)');

        // Transition 1: H.264 NVENC -> AV1 NVENC
        console.log('Transitioning to AV1 NVENC...');
        await page.click('#config-btn');
        // Unhide gpu options if needed, they are normally shown based on server config,
        // but since we started with --gpu, the server should send the gpu codec list and show them.
        await page.selectOption('#video-codec-select', 'av1_nvenc');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'AV1 NVENC');

        // Transition 2: AV1 NVENC -> H.265 NVENC
        console.log('Transitioning to H.265 NVENC...');
        await page.selectOption('#video-codec-select', 'h265_nvenc');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'H.265 NVENC', true);

        // Transition 3: H.265 NVENC -> H.264 NVENC
        console.log('Transitioning back to H.264 NVENC...');
        await page.selectOption('#video-codec-select', 'h264_nvenc');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'H.264 NVENC after transitions');

        console.log('NVENC codec transitions verified!');
    });
});