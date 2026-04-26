import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { readClientStats, waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-framerate-test';
const PORT = '8084';

test.describe('Wayland Dynamic Framerate E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for Wayland framerate test...');
    execSync(`PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
    
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should toggle framerate and verify it', async ({ page }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width: 1280, height: 819 });
    await page.goto(`http://localhost:${PORT}`);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    // Open config menu
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    // Disable VBR and Damage Tracking to ensure constant frames for verification
    const qualityTabLocator = page.locator('.config-tab-btn[data-tab="tab-quality"]');
    await qualityTabLocator.click();
    await page.uncheck('#vbr-checkbox');
    await page.uncheck('#damage-tracking-checkbox');

    const streamTabLocator = page.locator('.config-tab-btn[data-tab="tab-stream"]');
    await streamTabLocator.click();

    // Select 60 FPS
    console.log('Selecting 60 FPS...');
    await page.selectOption('#framerate-select', '60');

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 20000,
    }).toContain('Received framerate config: 60 fps');

    // Select 15 FPS
    console.log('Selecting 15 FPS...');
    await page.selectOption('#framerate-select', '15');

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 20000,
    }).toContain('Received framerate config: 15 fps');

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 20000,
    }).toMatch(/Framerate: 15|Using video filter: fps=15|-r 15\b/);

    await expect.poll(async () => {
      const stats = await readClientStats(page);
      console.log(`Browser-reported FPS after 15 FPS switch: ${stats.fps}`);
      return stats.fps > 5 && stats.fps < 25;
    }, { timeout: 30000 }).toBe(true);

    // Verify decoded browser frames reflect the new server framerate.
    await expect.poll(async () => {
      const before = await readClientStats(page);
      const start = Date.now();
      await page.waitForTimeout(4000);
      const after = await readClientStats(page);
      const elapsedMs = Date.now() - start;
      const deltaDecoded = after.totalDecoded - before.totalDecoded;
      const decodedFps = (deltaDecoded * 1000) / elapsedMs;
      console.log(`Measured decoded FPS after 15 FPS switch: ${decodedFps.toFixed(1)} (${deltaDecoded} frames)`);
      return deltaDecoded >= 5 && decodedFps > 5 && decodedFps < 25;
    }, { timeout: 30000 }).toBe(true);

    // Verify it still says WebRTC and decoding continues
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});
