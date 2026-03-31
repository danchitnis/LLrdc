import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-input-comp-test';
const PORT = '8084';

test.describe('Wayland Comprehensive Input Verification', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for input verification...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 -e USE_DEBUG_INPUT=true danchitnis/llrdc:latest`);
    
    // Give it time to boot
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should verify comprehensive mouse and keyboard input', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);
    
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    const displayContainer = page.locator('#display-container');
    const box = await displayContainer.boundingBox();
    if (!box) throw new Error('Could not find display container');

    // 1. Mouse movement
    await page.mouse.move(box.x + 100, box.y + 100);
    await page.waitForTimeout(500);

    // 2. Right click (context menu)
    await page.mouse.click(box.x + 200, box.y + 200, { button: 'right' });
    await page.waitForTimeout(500);

    // 3. Middle click
    await page.mouse.click(box.x + 300, box.y + 300, { button: 'middle' });
    await page.waitForTimeout(500);

    // 4. Keyboard input
    // First focus the container (which calls overlayEl.focus())
    await displayContainer.click();
    await page.keyboard.type('Hello Wayland');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(500);

    // 5. Scroll wheel
    await page.mouse.wheel(0, 100); // Vertical scroll
    await page.waitForTimeout(200);
    await page.mouse.wheel(50, 0);  // Horizontal scroll
    await page.waitForTimeout(5000);

    // Verify logs
    const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    console.log('--- CONTAINER INPUT LOGS ---');
    // Filter logs to show only Wayland input actions for clarity
    const inputLogs = logs.split('\n').filter(l => l.includes('Wayland')).join('\n');
    console.log(inputLogs);
    console.log('--- END LOGS ---');

    // Assertions
    expect(inputLogs).toContain('Wayland mouse move');
    expect(inputLogs).toContain('Wayland mouse button 272 mousedown'); // Left
    expect(inputLogs).toContain('Wayland mouse button 273 mousedown'); // Right
    expect(inputLogs).toContain('Wayland mouse button 274 mousedown'); // Middle
    expect(inputLogs).toContain('Wayland key KeyH (35) keydown');      // First char of "Hello"
    expect(inputLogs).toContain('Wayland key Enter (28) keydown');     // Enter key
    expect(logs).toMatch(/axis 0 100/); // Vertical scroll
    expect(logs).toMatch(/axis 1 50/);  // Horizontal scroll
  });
});
