import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-mouse-test';
const PORT = '8082';

test.describe('Wayland Mouse E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for mouse test...');
    const containerImage = process.env.CONTAINER_IMAGE || 'danchitnis/llrdc:latest';
    const [imageName, imageTag] = containerImage.split(':');
    execSync(`IMAGE_NAME=${imageName} IMAGE_TAG=${imageTag || 'latest'} PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net --debug-input --res 1080p`, { stdio: 'inherit' });
    
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should verify mouse movement via container logs', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`http://localhost:${PORT}`);
    
    const statusEl = page.locator('#status');
    await expect(statusEl).toContainText(/\[.+\]/i, { timeout: 30000 });

    // Wait for the remote stream to actually be 1920x1080 (forced by --res 1080p at startup).
    await expect.poll(async () => {
        return await page.evaluate(() => {
            const canvas = document.getElementById('display') as HTMLCanvasElement;
            return { width: canvas.width, height: canvas.height };
        });
    }, { timeout: 30000 }).toMatchObject({ width: 1920, height: 1080 });

    const box = await page.evaluate(() => {
        const el = document.getElementById('display-container');
        if (!el) return null;
        const rect = el.getBoundingClientRect();
        return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    });
    if (!box) throw new Error('Could not find display container bounding box');

    const targetX = box.x + 500;
    const targetY = box.y + 300;

    console.log(`Moving mouse to element relative 500, 300 (Page: ${targetX}, ${targetY})...`);
    await page.mouse.move(targetX, targetY);
    await page.waitForTimeout(500);

    let logs = '';
    await expect.poll(async () => {
        logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
        return logs;
    }, {
        timeout: 10000,
        intervals: [500, 1000, 2000],
    }).toContain('Wayland mouse move:');

    const moveIndex = logs.indexOf('Wayland mouse move:');
    const firstMouseDownIndex = logs.indexOf('"action":"mousedown"');
    expect(moveIndex).toBeGreaterThanOrEqual(0);
    if (firstMouseDownIndex >= 0) {
        expect(moveIndex).toBeLessThan(firstMouseDownIndex);
    }

    await page.mouse.down();
    await page.waitForTimeout(500);
    await page.mouse.up();
    await page.waitForTimeout(1000);

    // Verify logs with retries
    await expect.poll(async () => {
        logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
        return logs;
    }, {
        timeout: 10000,
        intervals: [500, 1000, 2000],
    }).toContain('Wayland mouse button 272 mouseup');

    console.log('--- CONTAINER LOGS ---');
    console.log(logs);
    console.log('--- END LOGS ---');
    
    expect(logs).toContain('Wayland mouse move:');
    // Also check that we received the mousedown message
    expect(logs).toContain('"action":"mousedown"');
    
    await expect(statusEl).toContainText(/\[.+\]/i);
  });

  test('should handle rapid mouse movements without stalling', async ({ page }) => {
    test.setTimeout(60000);
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`http://localhost:${PORT}`);
    
    const statusEl = page.locator('#status');
    await expect(statusEl).toContainText(/\[.+\]/i, { timeout: 30000 });

    // Wait for the remote stream to actually be 1920x1080 (forced by --res 1080p at startup).
    await expect.poll(async () => {
        return await page.evaluate(() => {
            const canvas = document.getElementById('display') as HTMLCanvasElement;
            return { width: canvas.width, height: canvas.height };
        });
    }, { timeout: 30000 }).toMatchObject({ width: 1920, height: 1080 });

    const box = await page.evaluate(() => {
        const el = document.getElementById('display-container');
        if (!el) return null;
        const rect = el.getBoundingClientRect();
        return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    });
    if (!box) throw new Error('Could not find display container bounding box');

    console.log('Dispatching 1000 rapid mousemove events...');
    const duration = await page.evaluate(async (b) => {
        const start = Date.now();
        const overlay = document.querySelector('#video-container') || document.body;
        for (let i = 0; i < 1000; i++) {
            const ev = new MouseEvent('mousemove', {
                clientX: b.x + 100 + (i % 100),
                clientY: b.y + 100 + (i % 100),
                bubbles: true,
            });
            overlay.dispatchEvent(ev);
            // minimal delay to allow event loop
            if (i % 10 === 0) await new Promise(r => setTimeout(r, 1));
        }
        return Date.now() - start;
    }, box);
    
    console.log(`Dispatched 1000 mouse moves in ${duration}ms`);

    expect(duration).toBeLessThan(5000);
  });
});
