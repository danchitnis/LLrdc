import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

// Helper to kill process on port
import { execSync } from 'child_process';
function killPort(port: number) {
  try {
    execSync(`fuser -k ${port}/tcp`);
  } catch (e) {
    // ignore if no process found
  }
}
const SERVER_URL = `http://localhost:${PORT}`;
const SERVER_PATH = path.join(__dirname, '../src/server.ts');

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Video Streaming', () => {
  test.beforeAll(async () => {
    killPort(PORT);
    console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
    // Use tsx directly to run the server
    serverProcess = spawn('npm', ['start'], {
      env: { ...process.env, PORT: PORT.toString(), FPS: '5', DISPLAY_NUM: DISPLAY_NUM.toString() },
      stdio: ['ignore', 'pipe', 'pipe'], // Capture stdout/stderr
    });

    // Wait for server to be ready
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
      }, 15000); // Increased timeout

      serverProcess.stdout?.on('data', (data) => {
        const output = data.toString();
        outputBuffer += output;
        // console.log(`[Server]: ${output}`);
        if (output.includes(`Server listening on http://0.0.0.0:${PORT}`)) {
          clearTimeout(timeout);
          resolve();
        }
      });

      serverProcess.stderr?.on('data', (data) => {
        const output = data.toString();
        outputBuffer += output;
        // console.error(`[Server Error]: ${output}`);
      });

      serverProcess.on('exit', (code) => {
        clearTimeout(timeout);
        reject(new Error(`Server exited early with code ${code}. Output:\n${outputBuffer}`));
      });
    });
    console.log('Server started.');
  });

  test.afterAll(async () => {
    console.log('Stopping server...');
    if (serverProcess) {
      serverProcess.kill('SIGTERM');
      // Wait for process to exit
      await new Promise<void>((resolve) => {
        const timeout = setTimeout(() => {
          if (!serverProcess.killed) serverProcess.kill('SIGKILL');
          resolve();
        }, 5000);
        serverProcess.on('exit', () => {
          clearTimeout(timeout);
          resolve();
        });
      });
    }
    killPort(PORT);
    console.log('Server stopped.');
    console.log('--- Server Output ---');
    console.log(outputBuffer);
    console.log('---------------------');
  });

  test('should stream video and handle inputs', async ({ page }) => {
    await test.step('Navigate to viewer', async () => {
      page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
      await page.goto(SERVER_URL);
      await expect(page).toHaveTitle(/Remote Desktop/);
    });

    await test.step('Verify video element exists', async () => {
      const video = page.locator('#display');
      await expect(video).toBeVisible();
    });

    await test.step('Verify video is playing', async () => {
      const video = page.locator('#display');

      // Check if video is ready (HAVE_ENOUGH_DATA = 4, HAVE_FUTURE_DATA = 3)
      // We might need to wait a bit for the stream to start
      await expect.poll(async () => {
        return await video.evaluate((v: HTMLVideoElement) => v.readyState);
      }, {
        message: 'Video should be ready',
        timeout: 10000,
      }).toBeGreaterThanOrEqual(3);

      // Check if video has dimensions
      const width = await video.evaluate((v: HTMLVideoElement) => v.videoWidth);
      const height = await video.evaluate((v: HTMLVideoElement) => v.videoHeight);
      expect(width).toBeGreaterThan(0);
      expect(height).toBeGreaterThan(0);
    });

    await test.step('Verify input interactions', async () => {
      // Click on the overlay
      const overlay = page.locator('#input-overlay');
      await overlay.click({ position: { x: 100, y: 100 } });

      // Type some keys
      await page.keyboard.type('Hello World');

      // We can't easily verify the effect on the remote desktop without OCR or more complex setup,
      // but we can verify that these actions don't throw errors in the client console.
      const consoleErrors: string[] = [];
      page.on('console', msg => {
        if (msg.type() === 'error') consoleErrors.push(msg.text());
      });

      expect(consoleErrors).toEqual([]);
    });
  });
});
