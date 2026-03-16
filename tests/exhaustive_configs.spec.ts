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

test.describe('Exhaustive Configuration Transitions', () => {
    const CONTAINER_NAME = `llrdc-exhaustive-${PORT}`;

    test.beforeAll(async () => {
        killPort(PORT);
        execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        console.log(`Starting server on port ${PORT}...`);
        
        serverProcess = spawn('./docker-run.sh', [], {
            env: { 
                ...process.env, 
                PORT: PORT.toString(), 
                HOST_PORT: PORT.toString(),
                DISPLAY_NUM: DISPLAY_NUM.toString(),
                CONTAINER_NAME: CONTAINER_NAME,
                TEST_PATTERN: '1',
                USE_DEBUG_FFMPEG: '1'
            },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
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
        execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
    });

    const verifyStreaming = async (page: any, message: string) => {
        console.log(`Verifying: ${message}`);
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

    test('should survive multiple codec and framerate transitions', async ({ page }) => {
        page.on('console', msg => {
            if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
        });

        await page.goto(SERVER_URL);
        await page.click('body'); // Interaction for autoplay

        await verifyStreaming(page, 'Initial state (VP8 30fps)');

        // Transition 1: VP8 -> H.264
        console.log('Transitioning to H.264...');
        await page.click('#config-btn');
        await page.selectOption('#video-codec-select', 'h264');
        await page.waitForTimeout(2000); // Give it a moment to trigger
        await verifyStreaming(page, 'H.264 30fps');

        // Transition 1.5: H.264 -> H.265
        console.log('Transitioning to H.265...');
        await page.selectOption('#video-codec-select', 'h265');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'H.265 30fps');

        // Transition 2: H.265 -> AV1
        console.log('Transitioning to AV1...');
        await page.selectOption('#video-codec-select', 'av1');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'AV1 30fps');

        // Transition 3: AV1 -> VP8 (and change framerate)
        console.log('Transitioning to VP8 @ 15fps...');
        await page.selectOption('#video-codec-select', 'vp8');
        await page.selectOption('#framerate-select', '15');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'VP8 15fps');

        // Transition 4: VP8 -> AV1 (and change framerate back to 30)
        console.log('Transitioning back to AV1 @ 30fps...');
        await page.selectOption('#video-codec-select', 'av1');
        await page.selectOption('#framerate-select', '30');
        await page.waitForTimeout(2000);
        await verifyStreaming(page, 'AV1 30fps after framerate change');

        console.log('Exhaustive configuration transitions verified!');
    });
});
