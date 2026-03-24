import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-wayland-test';
const PORT = '8081';

test.describe('Minimal Wayland E2E', () => {
  test.beforeAll(async () => {
    // Ensure any dangling container from a previous failed run is removed
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }

    console.log('Starting container...');
    execSync(`docker run -d --rm --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 llrdc:latest`);
    
    // Give it a moment to boot
    await new Promise(r => setTimeout(r, 3000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }
  });

  test('should establish WebRTC and handle mouse click', async ({ page }) => {
    // 1. Load the page
    await page.goto(`http://localhost:${PORT}`);
    
    // 2. Wait for WebRTC connection
    // The status element shows "WebRTC Connected" when active
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 15000 });

    // 3. Simulate mouse click
    // Our Wayland implementation has an xfce4-terminal running in the background.
    // Clicking on it might do something, or at least shouldn't crash.
    const displayContainer = page.locator('#display-container');
    await displayContainer.click({ position: { x: 100, y: 100 } });
    
    // Wait a bit to ensure no crash
    await page.waitForTimeout(1000);
    
    // We confirm that we are still connected
    await expect(statusEl).toHaveText(/WebRTC/i);
    
    // Verification complete
  });
});
