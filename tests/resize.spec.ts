import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import net from 'net';

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

    const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

    serverProcess = spawn('npm', ['start'], {
        env: {
            ...process.env,
            PORT: String(serverPort),
            FPS: '30',
            DISPLAY_NUM: DISPLAY_NUM.toString()
        },
        stdio: 'pipe',
        detached: false
    });

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server Error]: ${data}`));

    try {
        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => reject(new Error('Timeout waiting for server start')), 20000);
            const dataHandler = (data: any) => {
                if (data.toString().includes('Server listening on')) {
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

async function getDisplaySizes(page: any) {
    return page.evaluate(() => {
        const container = document.getElementById('display-container') as HTMLDivElement;
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const scale = window.devicePixelRatio || 1;
        return {
            expectedW: Math.round(container.clientWidth * scale),
            expectedH: Math.round(container.clientHeight * scale),
            canvasW: canvas.width,
            canvasH: canvas.height
        };
    });
}

async function getCanvasBrightness(page: any) {
    return page.evaluate(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const ctx = canvas.getContext('2d');
        if (!ctx || canvas.width === 0 || canvas.height === 0) {
            return 0;
        }
        const points = [
            [0.1, 0.1],
            [0.5, 0.5],
            [0.9, 0.1],
            [0.1, 0.9],
            [0.9, 0.9]
        ];
        let total = 0;
        for (const [px, py] of points) {
            const x = Math.min(canvas.width - 1, Math.max(0, Math.floor(canvas.width * px)));
            const y = Math.min(canvas.height - 1, Math.max(0, Math.floor(canvas.height * py)));
            const data = ctx.getImageData(x, y, 1, 1).data;
            total += data[0] + data[1] + data[2];
        }
        return total;
    });
}

test('resizes desktop to match browser window', async ({ page }) => {
    test.setTimeout(45000);
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));

    await page.setViewportSize({ width: 1100, height: 800 });
    await page.goto(serverUrl);

    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).getStats?.().totalDecoded || 0);
    }, {
        message: 'Video should be decoding before resize validation',
        timeout: 20000
    }).toBeGreaterThan(0);

    await expect.poll(async () => {
        return await getCanvasBrightness(page);
    }, {
        message: 'Canvas should render a non-blank test pattern',
        timeout: 20000
    }).toBeGreaterThan(0);

    await expect.poll(async () => {
        const sizes = await getDisplaySizes(page);
        const widthOk = Math.abs(sizes.canvasW - sizes.expectedW) <= 2;
        const heightOk = Math.abs(sizes.canvasH - sizes.expectedH) <= 2;
        return widthOk && heightOk;
    }, {
        message: 'Canvas size should match initial viewport',
        timeout: 20000
    }).toBe(true);

    await page.setViewportSize({ width: 900, height: 700 });

    await expect.poll(async () => {
        const sizes = await getDisplaySizes(page);
        const widthOk = Math.abs(sizes.canvasW - sizes.expectedW) <= 2;
        const heightOk = Math.abs(sizes.canvasH - sizes.expectedH) <= 2;
        return widthOk && heightOk;
    }, {
        message: 'Canvas size should match resized viewport',
        timeout: 20000
    }).toBe(true);

    await expect.poll(async () => {
        return await getCanvasBrightness(page);
    }, {
        message: 'Canvas should still render after resize',
        timeout: 20000
    }).toBeGreaterThan(0);
});
