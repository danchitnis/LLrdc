import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-wayland-mouse-test';
const PORT = '8082';

test.describe('Wayland Mouse E2E', () => {
  test.beforeAll(async () => {
    try {
      execSync(`docker rm -f ${CONTAINER_NAME}`);
    } catch (e) {}

    console.log('Starting container for mouse test...');
    // No longer need --device /dev/uinput or SYS_ADMIN as we use Wayland protocols.
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 -e USE_DEBUG_INPUT=true danchitnis/llrdc:wayland-latest`);
    
    await new Promise(r => setTimeout(r, 10000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME}`);
    } catch (e) {}
  });

  test('should verify mouse movement via container logs', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);
    
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    const displayContainer = page.locator('#display-container');
    
    const box = await displayContainer.boundingBox();
    if (!box) throw new Error('Could not find display container bounding box');

    const targetX = box.x + 500;
    const targetY = box.y + 300;

    console.log(`Moving mouse to element relative 500, 300 (Page: ${targetX}, ${targetY})...`);
    await page.mouse.move(targetX, targetY);
    await page.waitForTimeout(500);
    await page.mouse.click(targetX, targetY);

    await page.waitForTimeout(2000);

    // Verify logs
    const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    console.log('--- CONTAINER LOGS ---');
    console.log(logs);
    console.log('--- END LOGS ---');
    
    // Check for "Wayland mouse move: 490, 300" (or close)
    expect(logs).toContain('Wayland mouse move:');
    expect(logs).toContain('Wayland mouse button 272 mousedown');
    
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});
