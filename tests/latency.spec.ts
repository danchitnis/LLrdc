
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

async function waitForPort(port: number, timeout = 10000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    try {
      await new Promise<void>((resolve, reject) => {
        const socket = new net.Socket();
        socket.setTimeout(200);
        socket.on('connect', () => {
          socket.destroy();
          resolve();
        });
        socket.on('timeout', () => {
          socket.destroy();
          reject(new Error('timeout'));
        });
        socket.on('error', (err) => {
          socket.destroy();
          reject(err);
        });
        socket.connect(port, 'localhost');
      });
      return;
    } catch (e) {
      await new Promise(r => setTimeout(r, 100));
    }
  }
  throw new Error(`Port ${port} not ready after ${timeout}ms`);
}

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
    // Wait for server to be ready via logs
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

test('measure end-to-end mouse latency', async ({ page }) => {
  test.setTimeout(60000);
  page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
  await page.goto(serverUrl);

  // Wait for stream
  await page.waitForFunction(() => {
    const stats = (window as any).getStats();
    return stats && stats.fps > 0;
  }, null, { timeout: 15000 });

  // Inject helper function to detect pixel change
  await page.evaluate(() => {
    (window as any).monitorPixelChange = (x: number, y: number, width: number, height: number, timeout: number = 5000) => {
      return new Promise((resolve, reject) => {
        const display = document.getElementById('display') as HTMLVideoElement | HTMLCanvasElement;
        const canvas = document.createElement('canvas');
        // Handle both video (videoWidth/Height) and canvas (width/height)
        const dWidth = (display as HTMLVideoElement).videoWidth || (display as HTMLCanvasElement).width;
        const dHeight = (display as HTMLVideoElement).videoHeight || (display as HTMLCanvasElement).height;

        canvas.width = dWidth;
        canvas.height = dHeight;

        const ctx = canvas.getContext('2d', { willReadFrequently: true });
        if (!ctx) return reject('No context');

        // Get baseline of the region
        ctx.drawImage(display, 0, 0);
        const baseline = ctx.getImageData(x, y, width, height).data;
        const start = performance.now();
        let maxDiff = 0;

        function check() {
          if (performance.now() - start > timeout) return resolve({ time: -1, maxDiff }); // Timeout

          ctx!.drawImage(display, 0, 0);
          const current = ctx!.getImageData(x, y, width, height).data;

          let diffSum = 0;
          let changedPixels = 0;

          for (let i = 0; i < baseline.length; i += 4) {
            const d = Math.abs(current[i] - baseline[i]) +
              Math.abs(current[i + 1] - baseline[i + 1]) +
              Math.abs(current[i + 2] - baseline[i + 2]);
            if (d > 30) {
              changedPixels++;
              diffSum += d;
            }
          }

          if (changedPixels > 10) { // Threshold: at least 10 pixels changed
            resolve({ time: performance.now(), maxDiff: diffSum });
          } else {
            // Check aggressively
            requestAnimationFrame(check);
          }
        }
        check();
      });
    };
  });

  // Spawn xeyes
  await page.evaluate(() => {
    const ws = new WebSocket(window.location.href.replace('http', 'ws'));
    ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
  });
  await page.waitForTimeout(2000);

  // Test sequence
  const iterations = 5;
  const latencies: number[] = [];

  // Coordinates
  const startX = 100;
  const startY = 100;
  const targetX = 600;
  const targetY = 400;

  // Ensure we target the overlay correctly
  const overlay = page.locator('#input-overlay');

  for (let i = 0; i < iterations; i++) {
    // 1. Move to start position (relative to overlay)
    await overlay.hover({ position: { x: startX, y: startY } });
    await page.waitForTimeout(1000); // Wait for settle

    // Start monitoring entire screen for simplicity (or a large central region)
    // xeyes should be visible.
    const detectionPromise = page.evaluate(() => {
      return (window as any).monitorPixelChange(0, 0, 1280, 720);
    });

    const moveStart = await page.evaluate(() => performance.now());

    // 3. Move to target (relative to overlay)
    await overlay.hover({ position: { x: targetX, y: targetY } });

    // 4. Wait for detection
    const result: any = await detectionPromise;
    const detectionTime = result.time;

    if (detectionTime === -1) {
      console.log(`Iteration ${i}: Timeout. Max diff: ${result.maxDiff}`);
    } else {
      const latency = (detectionTime as number) - (moveStart as number);
      console.log(`Iteration ${i}: Latency ${latency.toFixed(2)}ms`);
      latencies.push(latency);
    }
  }

  latencies.sort((a, b) => a - b);
  const mid = Math.floor(latencies.length / 2);
  const median = latencies.length % 2 !== 0 ? latencies[mid] : (latencies[mid - 1] + latencies[mid]) / 2;

  console.log(`Median End-to-End Latency: ${median.toFixed(2)}ms`);

  // Expect median latency to be reasonable (e.g. < 500ms in this env)
  expect(median).toBeLessThan(500);
});
