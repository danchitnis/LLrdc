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

test.describe('Software H.264 Streaming', () => {
  test.beforeAll(async () => {
    killPort(PORT);
    console.log(`Starting server with VIDEO_CODEC=h264 on port ${PORT}...`);
    
    serverProcess = spawn('./docker-run.sh', [], {
      env: { 
        ...process.env, 
        PORT: PORT.toString(), 
        HOST_PORT: PORT.toString(),
        DISPLAY_NUM: DISPLAY_NUM.toString(),
        VIDEO_CODEC: 'h264',
        TEST_PATTERN: '1'
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`Server start timeout. Output:
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
  });

  test('should use libx264 and decode successfully', async ({ page }) => {
    page.on('console', msg => {
        if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
    });

    await page.goto(SERVER_URL);
    
    const status = page.locator('#status');
    await expect(status).toContainText(/h264/i, { timeout: 15000 });

    // Verify frames are decoding
    await expect.poll(async () => {
      return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
      message: 'Video should be decoding H.264 frames',
      timeout: 20000,
    }).toBeGreaterThan(0);
    
    console.log('Software H264 streaming verified!');
  });
});
