import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

import { execSync } from 'child_process';
function killPort(port: number) {
  try {
    execSync(`fuser -k ${port}/tcp`);
  } catch (e) {
    // ignore if no process found
  }
}
const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('HDPI Scaling', () => {
  test.beforeAll(async () => {
    killPort(PORT);
    console.log(`Starting server with HDPI=200 on port ${PORT} display :${DISPLAY_NUM}...`);
    serverProcess = spawn('npm', ['start'], {
      env: { ...process.env, PORT: PORT.toString(), FPS: '5', DISPLAY_NUM: DISPLAY_NUM.toString(), HDPI: '200' },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
      }, 20000);

      serverProcess.stdout?.on('data', (data) => {
        const output = data.toString();
        outputBuffer += output;
        if (output.includes(`Server listening on`)) {
          clearTimeout(timeout);
          resolve();
        }
      });

      serverProcess.stderr?.on('data', (data) => {
        const output = data.toString();
        outputBuffer += output;
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
  });

  test('should verify HDPI scaling applied in logs', async () => {
    // Verify that the server applied the HDPI settings
    expect(outputBuffer).toContain('Applying HDPI scaling: 200% (DPI: 192)');
  });

  test('should stream video successfully and change HDPI dynamically', async ({ page }) => {
    await page.goto(SERVER_URL);
    await expect(page).toHaveTitle(/Remote Desktop/);

    const display = page.locator('#display');
    await expect(display).toBeVisible();

    // Check if video frames have been decoded
    await expect.poll(async () => {
      return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
      message: 'Video should be decoding and rendering frames with HDPI enabled',
      timeout: 15000,
    }).toBeGreaterThan(0);

    // Verify initial dropdown value matches the HDPI=200 we started with
    await expect.poll(async () => {
        return await page.inputValue('#hdpi-select');
    }, {
        message: 'Initial HDPI dropdown value should be 200',
        timeout: 10000,
    }).toBe('200');

    // Click config button
    await page.click('#config-btn');
    
    // Change HDPI to 150%
    const hdpiSelect = page.locator('#hdpi-select');
    await hdpiSelect.selectOption('150');

    // Verify that the server receives the new config and applies it
    await expect.poll(() => outputBuffer, {
      message: 'Server should apply dynamic 150% HDPI scaling',
      timeout: 10000,
    }).toContain('Applying HDPI scaling: 150% (DPI: 144)');
  });
});

test.describe('HDPI Default Mapping', () => {
  const DEFAULT_PORT = 9000 + Math.floor(Math.random() * 500);
  let defaultServerProcess: ChildProcess;
  let defaultOutputBuffer = '';

  test.beforeAll(async () => {
    console.log(`Starting server with default HDPI on port ${DEFAULT_PORT}...`);
    defaultServerProcess = spawn('npm', ['start'], {
      env: { ...process.env, PORT: DEFAULT_PORT.toString(), FPS: '5', DISPLAY_NUM: '150' },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Default server timeout')), 20000);
      defaultServerProcess.stdout?.on('data', (data) => {
        defaultOutputBuffer += data.toString();
        if (data.toString().includes('Server listening on')) {
          clearTimeout(timeout);
          resolve();
        }
      });
    });
  });

  test.afterAll(async () => {
    if (defaultServerProcess) defaultServerProcess.kill('SIGTERM');
  });

  test('should verify default HDPI 0 maps to 100% in dropdown', async ({ page }) => {
    page.on('console', msg => console.log(`[Browser]: ${msg.text()}`));
    await page.goto(`http://localhost:${DEFAULT_PORT}`);
    
    // Wait for the dropdown to be updated by the initial config message
    await expect.poll(async () => {
      return await page.inputValue('#hdpi-select');
    }, {
      message: 'HDPI dropdown should be initialized to 100% (mapping from 0)',
      timeout: 10000,
    }).toBe('100');
  });
});
