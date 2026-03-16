import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 150 + Math.floor(Math.random() * 50);
const CONTAINER_NAME = `llrdc-test-vp8-${PORT}`;

function killPort(port: number) {
  try {
    execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
  } catch (e) {}
}

const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Software VP8 Streaming', () => {
  test.beforeAll(async () => {
    killPort(PORT);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
    } catch (e) {}
    console.log(`Starting server with VIDEO_CODEC=vp8 on port ${PORT}...`);
    
    serverProcess = spawn('./docker-run.sh', [], {
      env: { 
        ...process.env, 
        PORT: PORT.toString(), 
        HOST_PORT: PORT.toString(),
        DISPLAY_NUM: DISPLAY_NUM.toString(),
        CONTAINER_NAME: CONTAINER_NAME,
        VIDEO_CODEC: 'vp8',
        TEST_PATTERN: '1'
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
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
           reject(new Error(`Server exited with code ${code}. Output:\n${outputBuffer}`));
        }
      });
    });
  });

  test.afterAll(async () => {
    if (serverProcess) {
      serverProcess.kill('SIGTERM');
    }
    killPort(PORT);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
    } catch (e) {}
  });

  test('should use libvpx and decode successfully without freezing', async ({ page }) => {
    page.on('console', msg => {
        if (msg.type() === 'log') console.log(`[Browser] ${msg.text()}`);
    });

    await page.goto(SERVER_URL);
    
    const status = page.locator('#status');
    await expect(status).toContainText(/vp8/i, { timeout: 15000 });

    // Verify frames are decoding
    await expect.poll(async () => {
      return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
      message: 'Video should be decoding VP8 frames',
      timeout: 20000,
    }).toBeGreaterThan(0);
    
    // Verify frames are continuing to decode (no freeze)
    const initialDecoded = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    await page.waitForTimeout(2000); // Wait 2 seconds
    const finalDecoded = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    
    expect(finalDecoded).toBeGreaterThan(initialDecoded);
    console.log(`VP8 frames decoded smoothly: ${initialDecoded} -> ${finalDecoded}`);
    console.log('Software VP8 streaming verified!');
  });
});
