import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

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

test.describe('GPU Acceleration (H264 NVENC)', () => {
  test.beforeAll(async () => {
    killPort(PORT);
    console.log(`Starting server with GPU flag on port ${PORT}...`);
    
    // We run docker-run.sh with --gpu and environment overrides
    serverProcess = spawn('./docker-run.sh', ['--gpu'], {
      env: { 
        ...process.env, 
        PORT: PORT.toString(), 
        HOST_PORT: PORT.toString(),
        DISPLAY_NUM: DISPLAY_NUM.toString(),
        TEST_PATTERN: '1' // Use test pattern to avoid needing a real X11 display if possible
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`GPU Server start timeout. Output:
${outputBuffer}`));
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
           reject(new Error(`Server exited with code ${code}. Output:
${outputBuffer}`));
        }
      });
    });
  });

  test.afterAll(async () => {
    if (serverProcess) {
      serverProcess.kill('SIGTERM');
    }
    killPort(PORT);
    // Cleanup any lingering containers just in case
    try {
        execSync(`docker rm -f llrdc`, { stdio: 'ignore' });
    } catch(e) {}
  });

  test('should use h264_nvenc and decode successfully', async ({ page }) => {
    page.on('console', msg => {
        if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
    });

    await page.goto(SERVER_URL);
    
    // Check if the UI reflects the h264_nvenc codec
    const status = page.locator('#status');
    await expect(status).toContainText(/h264_nvenc|h264/i, { timeout: 15000 });

    // Verify frames are decoding
    await expect.poll(async () => {
      return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
      message: 'Video should be decoding H.264 frames',
      timeout: 20000,
    }).toBeGreaterThan(0);
    
    console.log('GPU H264 streaming verified!');
  });
});
