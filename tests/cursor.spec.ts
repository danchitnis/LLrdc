import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';

const SERVER_PORT = 8080 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);
const SERVER_URL = `http://localhost:${SERVER_PORT}`;

test.describe('Cursor Shape Syncing', () => {
    let serverProcess: ChildProcess;

    test.beforeAll(async () => {
        console.log(`Starting server on port ${SERVER_PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            cwd: process.cwd(),
            env: { ...process.env, PORT: SERVER_PORT.toString(), DISPLAY_NUM: DISPLAY_NUM.toString() },
            stdio: 'pipe',
            detached: true,
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                if (serverProcess) serverProcess.kill();
                reject(new Error('Timeout waiting for server start'))
            }, 20000);

            serverProcess.stdout?.on('data', (data) => {
                const output = data.toString();
                console.log(`[SERVER]: ${output.trim()}`);
                if (output.includes(`Server listening on http://0.0.0.0:${SERVER_PORT}`)) {
                    clearTimeout(timeout);
                    resolve();
                }
            });

            serverProcess.stderr?.on('data', (data) => {
                console.error(`[SERVER ERR]: ${data.toString().trim()}`);
            });
        });
    });

    test.afterAll(() => {
        if (serverProcess && serverProcess.pid) {
            try {
                process.kill(-serverProcess.pid);
            } catch (e) {}
        }
    });

    test('should receive cursor shape updates from server', async ({ page }) => {
        await page.goto(SERVER_URL);

        // Wait for connection
        const status = page.locator('#status');
        await expect(status).toContainText(/Connected|FPS|Latency/, { timeout: 15000 });

        const overlay = page.locator('#input-overlay');
        await expect(overlay).toBeVisible();

        // Wait until it actually becomes a data URL (the default cursor from server)
        await expect.poll(async () => {
            return await overlay.evaluate(el => window.getComputedStyle(el).cursor);
        }, {
            timeout: 15000,
        }).toContain('data:image/png;base64');

        // Get initial cursor style
        const initialCursor = await overlay.evaluate(el => window.getComputedStyle(el).cursor);
        console.log(`Initial cursor: ${initialCursor.substring(0, 100)}...`);

        // Spawn mousepad to get a text input area
        console.log('Spawning mousepad...');
        await page.evaluate(() => {
            const ws = new WebSocket(window.location.href.replace('http', 'ws'));
            ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'mousepad' }));
        });

        // Wait for mousepad to open
        await page.waitForTimeout(3000);

        // Move the mouse to the center of the screen to hover over the text area
        const box = await overlay.boundingBox();
        if (box) {
             await overlay.hover({ position: { x: box.width / 2, y: box.height / 2 } });
        }

        // Check if cursor changed to a DIFFERENT data URL
        await expect.poll(async () => {
            const cursor = await overlay.evaluate(el => window.getComputedStyle(el).cursor);
            return cursor;
        }, {
            message: 'Wait for cursor to change from initial shape',
            timeout: 10000,
        }).not.toEqual(initialCursor);

        const updatedCursor = await overlay.evaluate(el => window.getComputedStyle(el).cursor);
        console.log(`Updated cursor: ${updatedCursor.substring(0, 100)}...`);
        
        expect(updatedCursor).toContain('data:image/png;base64');
    });
});
