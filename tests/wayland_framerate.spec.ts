import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-wayland-framerate-test';
const PORT = '8084';

test.describe('Wayland Dynamic Framerate E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for Wayland framerate test...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 danchitnis/llrdc:latest`);
    
    await new Promise(r => setTimeout(r, 30000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should change framerate dynamically', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    // Open config menu
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    // Disable VBR to ensure constant frames for verification
    const qualityTabLocator = page.locator('.config-tab-btn[data-tab="tab-quality"]');
    await qualityTabLocator.click();
    await page.uncheck('#vbr-checkbox');

    const streamTabLocator = page.locator('.config-tab-btn[data-tab="tab-stream"]');
    await streamTabLocator.click();

    // Select 60 FPS
    console.log('Selecting 60 FPS...');
    await page.selectOption('#framerate-select', '60');

    // Wait for propagation and ffmpeg restart
    await page.waitForTimeout(5000);

    let logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    expect(logs).toContain('Received framerate config: 60 fps');
    expect(logs).toContain('Config updated, sending Kill() to restart stream...');

    // Select 15 FPS
    console.log('Selecting 15 FPS...');
    await page.selectOption('#framerate-select', '15');

    await page.waitForTimeout(5000);

    logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    expect(logs).toContain('Received framerate config: 15 fps');

    // Verify it still says WebRTC and decoding continues
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});
