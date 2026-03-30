import { test, expect } from '@playwright/test';
import { execSync, spawn } from 'child_process';

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
    const containerImage = process.env.CONTAINER_IMAGE || 'danchitnis/llrdc:latest';
    execSync(`docker run -d --rm --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 ${containerImage}`);
    
    // Log container output
    spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
    
    // Give it a moment to boot
    await new Promise(r => setTimeout(r, 20000));
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
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    // 1. Load the page
    await page.goto(`http://localhost:${PORT}`);
    
    // 2. Wait for WebRTC connection
    // The status element shows "[WebRTC ...]" when active
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebRTC/i, { timeout: 20000 });

    // 3. Simulate mouse click
    // Our Wayland implementation has an xfce4-terminal running in the background.
    // Clicking on it might do something, or at least shouldn't crash.
    const displayContainer = page.locator('#display-container');
    await displayContainer.click({ position: { x: 100, y: 100 } });
    
    // Move the mouse and verify video frames are arriving (FPS > 0)
    await expect(async () => {
        await page.mouse.move(200 + Math.random() * 400, 150 + Math.random() * 300);
        await page.waitForTimeout(100);
        const status = await statusEl.textContent() || '';
        expect(status).toMatch(/\[WebRTC/i);
        const fpsMatch = status.match(/FPS: (\d+)/);
        const fps = fpsMatch ? parseInt(fpsMatch[1], 10) : 0;
        expect(fps).toBeGreaterThan(0);
    }).toPass({ timeout: 10000 });
    
    const finalStatus = await statusEl.textContent();
    console.log(`Final Status: ${finalStatus}`);
    
    // Verification complete
  });
});
