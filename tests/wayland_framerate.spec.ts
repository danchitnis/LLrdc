import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

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

    // Verify UI shows lower FPS
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        return match ? parseInt(match[1], 10) : 0;
    }, { timeout: 15000 }).toBeLessThan(25);

    // Verify it still says WebRTC and decoding continues
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});
