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

// Helper to find a free port
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

  // Use tsx to run the server directly
  serverProcess = spawn('npm', ['start'], {
    // env: { ...process.env, PORT: String(serverPort), FPS: '60' },
    env: { ...process.env, PORT: String(serverPort), FPS: '60', DISPLAY_NUM: DISPLAY_NUM.toString() },
    stdio: 'pipe', // Capture stdio for debugging if needed
    detached: false
  });

  serverProcess.stdout?.on('data', (data) => {
    // console.log(`[Server]: ${data}`);
  });

  serverProcess.stderr?.on('data', (data) => {
    // console.error(`[Server Error]: ${data}`);
  });

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
    // Give it a moment to clean up
    await new Promise(r => setTimeout(r, 1000));
    if (!serverProcess.killed) {
      serverProcess.kill('SIGKILL');
    }
  }
});

test('benchmark video stream performance', async ({ page }) => {
  // 2. Connect to server
  await page.goto(serverUrl);

  // 3. Wait for the video stream to start (FPS > 0)
  console.log('Waiting for video stream...');
  await page.waitForFunction(() => {
    const stats = (window as any).getStats();
    return stats && stats.fps > 0;
  }, null, { timeout: 15000 });

  // Spawn xeyes to ensure screen content changes
  console.log('Spawning xeyes...');
  await page.evaluate(() => {
    const ws = new WebSocket(window.location.href.replace('http', 'ws'));
    ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
  });
  // Give xeyes a moment to appear
  await page.waitForTimeout(2000);

  console.log('Stream started. Measuring for 10 seconds with mouse movement...');

  const statsData: { fps: number, latency: number }[] = [];
  const duration = 10000; // 10 seconds
  const interval = 1000; // Measure every second
  const startTime = Date.now();

  // Move mouse in a circle to force screen updates
  const centerX = 500;
  const centerY = 300;
  const radius = 100;
  let angle = 0;

  while (Date.now() - startTime < duration) {
    // Perform mouse movement
    const x = centerX + radius * Math.cos(angle);
    const y = centerY + radius * Math.sin(angle);
    await page.mouse.move(x, y);
    angle += 0.5;

    const stats = await page.evaluate(() => (window as any).getStats());
    statsData.push(stats);
    await page.waitForTimeout(interval);
  }

  // 4. Measure FPS and Latency
  const fpsValues = statsData.map(s => s.fps);
  const latencyValues = statsData.map(s => s.latency);

  const avgFps = fpsValues.reduce((a, b) => a + b, 0) / fpsValues.length;
  const minFps = Math.min(...fpsValues);
  const maxFps = Math.max(...fpsValues);

  const avgLatency = latencyValues.reduce((a, b) => a + b, 0) / latencyValues.length;
  const minLatency = Math.min(...latencyValues);
  const maxLatency = Math.max(...latencyValues);

  // 5. Log stats
  console.log('Benchmark Results:');
  console.log(`  FPS: Avg=${avgFps.toFixed(2)}, Min=${minFps}, Max=${maxFps}`);
  console.log(`  Latency (WebSocket RTT): Avg=${avgLatency.toFixed(2)}ms, Min=${minLatency}ms, Max=${maxLatency}ms`);

  // 6. Fails if Average FPS < 5
  expect(avgFps, `Average FPS (${avgFps.toFixed(2)}) should be >= 5`).toBeGreaterThanOrEqual(5);

  // 7. Fails if Average Latency > 200ms
  expect(avgLatency, `Average Latency (${avgLatency.toFixed(2)}ms) should be <= 200ms`).toBeLessThanOrEqual(200);
});
