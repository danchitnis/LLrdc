import { test, expect } from '@playwright/test';
import { execSync, spawn } from 'child_process';
import { waitForServerReady } from './helpers';

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
    execSync(`IMAGE_NAME=${containerImage.split(':')[0]} IMAGE_TAG=${containerImage.split(':')[1] || 'latest'} PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`, { stdio: 'inherit' });
    
    // Log container output
    spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
    
    await waitForServerReady(`http://localhost:${PORT}`);
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
    await page.setViewportSize({ width: 1280, height: 819 });
    // 1. Load the page
    await page.goto(`http://localhost:${PORT}`);
    
    // 2. Wait for WebRTC connection
    // The status element shows "[WebRTC ...]" when active
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebRTC/i, { timeout: 20000 });

    console.log('Switching to H.264 to verify it does not crash...');
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();
    await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();
    await page.evaluate(() => {
        const sel = document.getElementById('video-codec-select') as HTMLSelectElement;
        if (sel) {
            sel.value = 'h264';
            sel.dispatchEvent(new Event('change', { bubbles: true }));
        }
    });

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
        timeout: 20000,
    }).toContain('Target video codec changed to h264');

    await expect(statusEl).toHaveText(/\[WebRTC h264\]/i, { timeout: 30000 });

    // Close config dropdown
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).not.toBeVisible();

    // 3. Simulate mouse click
    // Our Wayland implementation has an xfce4-terminal running in the background.
    // Clicking on it might do something, or at least shouldn't crash.
    const displayContainer = page.locator('#display-container');
    await displayContainer.click({ position: { x: 100, y: 100 } });
    
    // Move the mouse and verify video frames are arriving (FPS > 0)
    await page.mouse.move(400, 300);
    await page.waitForTimeout(5000); // Wait for pollStats to get samples after codec change and connection reset

    await expect(async () => {
        await page.mouse.move(200 + Math.random() * 400, 150 + Math.random() * 300);
        await page.waitForTimeout(200);
        const status = await statusEl.textContent() || '';
        expect(status).toMatch(/\[WebRTC/i);
        const fpsMatch = status.match(/FPS: (\d+)/);
        const fps = fpsMatch ? parseInt(fpsMatch[1], 10) : 0;
        expect(fps).toBeGreaterThan(0);
    }).toPass({ timeout: 20000 });
    
    const finalStatus = await statusEl.textContent();
    console.log(`Final Status: ${finalStatus}`);
    
    // Verification complete
  });
});
