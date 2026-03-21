import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import net from 'net';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

let serverProcess: ChildProcess;
let serverPort: number;
let serverUrl: string;

async function getFreePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();
        server.unref();
        server.on('error', reject);
        server.listen(0, () => {
            const port = (server.address() as net.AddressInfo).port;
            server.close(() => resolve(port));
        });
    });
}

test.beforeAll(async () => {
    serverPort = await getFreePort();
    serverUrl = `http://localhost:${serverPort}`;
    console.log(`Starting server on port ${serverPort}...`);

    const serverPath = path.join(__dirname, '../src/server.ts');
    const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

    serverProcess = spawn('npm', ['start'], {
        env: { ...process.env, PORT: String(serverPort), FPS: '30', DISPLAY_NUM: DISPLAY_NUM.toString(), TEST_MINIMAL_X11: 'true' },
        stdio: 'pipe',
        detached: false
    });

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server Error]: ${data}`));

    try {
        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => reject(new Error('Timeout waiting for server start')), 20000);
            const dataHandler = (data: any) => {
                if (data.toString().includes(`Server listening on`)) {
                    clearTimeout(timeout);
                    resolve();
                }
            };
            serverProcess.stdout?.on('data', dataHandler);
            serverProcess.stderr?.on('data', dataHandler);
            serverProcess.on('exit', (code) => {
                if (code !== null && code !== 0) reject(new Error('Server failed to start'));
            });
        });
        console.log(`Server is ready on port ${serverPort}`);
    } catch (e) {
        console.error('Server failed to start');
        if (serverProcess) serverProcess.kill();
        throw e;
    }
});

test.afterAll(async () => {
    if (serverProcess) {
        console.log('Stopping server...');
        serverProcess.kill('SIGTERM');
        await new Promise(r => setTimeout(r, 1000));
        if (!serverProcess.killed) serverProcess.kill('SIGKILL');
    }
});

test('verify hybrid encoding overlay receives and clears patches', async ({ page }) => {
    test.setTimeout(30000);
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
    await page.goto(serverUrl);

    console.log('Waiting for stream to initialize...');
    await page.waitForTimeout(5000);

    // 1. Trigger motion by moving the mouse over the overlay
    console.log('Triggering motion...');
    const display = await page.locator('#input-overlay');
    await display.hover({ position: { x: 100, y: 100 } });
    await page.mouse.move(200, 200, { steps: 10 });
    
    // 2. While moving, the sharpness layer should be transparent (cleared)
    await expect.poll(async () => {
        return await page.evaluate(() => {
            const canvas = document.getElementById('sharpness-layer') as HTMLCanvasElement;
            if (!canvas) return false;
            const ctx = canvas.getContext('2d');
            if (!ctx) return false;
            const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
            for (let i = 3; i < pixels.length; i += 4) {
                if (pixels[i] !== 0) return false;
            }
            return true;
        });
    }, { timeout: 2000 }).toBe(true);

    // 3. Stop motion and wait for the settle timer (250ms on server + latency)
    console.log('Waiting for image to settle...');
    await page.waitForTimeout(1000);

    // 4. Check if the sharpness layer received the patch and is no longer transparent
    await expect.poll(async () => {
        return await page.evaluate(() => {
            const canvas = document.getElementById('sharpness-layer') as HTMLCanvasElement;
            if (!canvas) return false;
            const ctx = canvas.getContext('2d');
            if (!ctx) return false;
            const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
            for (let i = 3; i < pixels.length; i += 4) {
                if (pixels[i] !== 0) return true;
            }
            return false;
        });
    }, { timeout: 5000 }).toBe(true);
});

test('verify hybrid encoding can be disabled', async ({ page }) => {
    test.setTimeout(30000);
    await page.goto(serverUrl);

    console.log('Waiting for stream to initialize...');
    await page.waitForTimeout(5000);

    // 1. Open config and disable hybrid
    await page.click('#config-btn');
    await page.click('button[data-tab="tab-quality"]');
    
    const isChecked = await page.isChecked('#hybrid-checkbox');
    expect(isChecked).toBe(true); // Default should be on

    await page.uncheck('#hybrid-checkbox');
    await page.click('#config-btn'); // Close dropdown
    await page.waitForTimeout(1000); // Wait for config to propagate

    // 2. Clear the canvas manually in browser to ensure it's empty
    await page.evaluate(() => {
        const canvas = document.getElementById('sharpness-layer') as HTMLCanvasElement;
        const ctx = canvas.getContext('2d');
        if (ctx) ctx.clearRect(0, 0, canvas.width, canvas.height);
    });

    // 3. Wait and trigger some "motion" then wait for settle
    const display = await page.locator('#input-overlay');
    await display.hover({ position: { x: 100, y: 100 } });
    await page.mouse.move(200, 200, { steps: 5 });
    
    console.log('Waiting to see if patches arrive (they should not)...');
    await page.waitForTimeout(2000);

    // 4. Assert canvas is still empty
    const isEmpty = await page.evaluate(() => {
        const canvas = document.getElementById('sharpness-layer') as HTMLCanvasElement;
        if (!canvas) return true;
        const ctx = canvas.getContext('2d');
        if (!ctx) return true;
        const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
        for (let i = 3; i < pixels.length; i += 4) {
            if (pixels[i] !== 0) return false;
        }
        return true;
    });
    
    expect(isEmpty).toBe(true);
});

test('verify settle time config propagates', async ({ page }) => {
    test.setTimeout(30000);
    await page.goto(serverUrl);

    await page.waitForTimeout(5000);

    // 1. Open config and change settle time
    await page.click('#config-btn');
    await page.click('button[data-tab="tab-quality"]');
    
    // Using evaluate to ensure both slider move and event firing
    await page.evaluate(() => {
        const slider = document.getElementById('settle-slider') as HTMLInputElement;
        slider.value = '1200';
        slider.dispatchEvent(new Event('input'));
        slider.dispatchEvent(new Event('change'));
    });
    
    await page.waitForTimeout(2000);

    // 2. Refresh page to see if it persisted (initialConfig)
    await page.reload();
    await page.waitForTimeout(5000);
    
    await page.click('#config-btn');
    await page.click('button[data-tab="tab-quality"]');
    
    const settleValue = await page.inputValue('#settle-slider');
    expect(settleValue).toBe('1200');
});
