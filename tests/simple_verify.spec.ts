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

// Removed waitForPort

test.beforeAll(async () => {
    serverPort = await getFreePort();
    serverUrl = `http://localhost:${serverPort}`;
    console.log(`Starting server on port ${serverPort}...`);

    const serverPath = path.join(__dirname, '../src/server.ts');
    const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

    serverProcess = spawn('npm', ['start'], {
        env: { ...process.env, PORT: String(serverPort), FPS: '30', DISPLAY_NUM: DISPLAY_NUM.toString() },
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

test('verify video streaming', async ({ page }) => {
    test.setTimeout(30000);
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
    await page.goto(serverUrl);

    console.log('Waiting for stream to initialize...');

    // Wait for 20 seconds to allow connection and buffering
    await page.waitForTimeout(20000);

    const stats = await page.evaluate(() => (window as any).getStats());
    console.log('Stats:', JSON.stringify(stats, null, 2));

    expect(stats.totalDecoded).toBeGreaterThan(0);
});
