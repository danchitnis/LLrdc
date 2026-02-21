import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';

const SERVER_PORT = 8080 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);
const SERVER_URL = `http://localhost:${SERVER_PORT}`;

test.describe('Remote Desktop Mouse Interaction', () => {
    let serverProcess: ChildProcess;

    test.beforeAll(async () => {
        // Start the server
        console.log(`Starting server on port ${SERVER_PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            cwd: process.cwd(),
            env: { ...process.env, PORT: SERVER_PORT.toString(), DISPLAY_NUM: DISPLAY_NUM.toString() },
            stdio: 'pipe',
            detached: true,
        });

        // Wait for server to be ready
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

            serverProcess.on('error', (err) => {
                clearTimeout(timeout);
                reject(err);
            });

            serverProcess.on('exit', (code) => {
                if (code !== null && code !== 0) {
                    clearTimeout(timeout);
                    reject(new Error(`Server exited with code ${code}`));
                }
            });
        });
    });

    test.afterAll(() => {
        if (serverProcess) {
            console.log('Stopping server...');
            // Kill the process group to clean up children (sway, python, etc)
            if (serverProcess.pid) {
                try {
                    process.kill(-serverProcess.pid);
                } catch (e) {
                    // ignore if already dead
                }
            }
        }
    });

    test('should connect and send mouse events', async ({ page }) => {
        await page.goto(SERVER_URL);

        // Wait for connection
        const status = page.locator('#status');
        await expect(status).toContainText(/Connected|FPS|Latency/, { timeout: 15000 });

        // Interact with the overlay
        const overlay = page.locator('#input-overlay');
        await expect(overlay).toBeVisible();

        // Move mouse
        await overlay.hover({ position: { x: 100, y: 100 } });

        // Click (Left)
        await overlay.click({ button: 'left', position: { x: 100, y: 100 } });

        // Context Menu (Right Click)
        await overlay.click({ button: 'right', position: { x: 150, y: 150 } });

        // Allow some time for server to log events
        await page.waitForTimeout(1000);
    });
});
