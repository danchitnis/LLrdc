import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-keyboard-e2e-test';
const PORT = '8090';

test.describe('Wayland Keyboard Fast Typing E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(120000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for keyboard fast typing verification...');
    execSync(`PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
    
    await waitForServerReady(`http://localhost:${PORT}`, 60000);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should not drop characters during fast typing of complex strings', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 819 });
    await page.goto(`http://localhost:${PORT}`);
    
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    const displayContainer = page.locator('#display-container');

    // Launch mousepad (Wayland native)
    console.log('Spawning mousepad...');
    execSync(`docker exec -u remote -d -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} mousepad`);
    
    console.log('Waiting for mousepad to open...');
    await page.waitForTimeout(5000);

    // Click to focus
    await displayContainer.click({ position: { x: 400, y: 400 } });
    await page.waitForTimeout(1000);

    // Now type the complex string extremely fast
    const complexString = 'The Quick Brown Fox! 1234567890 @#$%^&*()_+{}|:<>?-=[]\\;\',./"';
    console.log(`Typing complex string: ${complexString}`);
    
    // Playwright's keyboard.type() doesn't emit 'ShiftLeft' keydown events for hardware code
    // listeners. We must explicitly press Shift for uppercase letters and symbols.
    for (const char of complexString) {
      if (char === char.toUpperCase() && char !== char.toLowerCase() || '!@#$%^&*()_+{}|:<>?~"'.includes(char)) {
        await page.keyboard.down('Shift');
        await page.keyboard.press(char);
        await page.keyboard.up('Shift');
      } else {
        await page.keyboard.press(char);
      }
    }
    
    await page.waitForTimeout(1000);

    // Explicitly send Ctrl+A and Ctrl+C
    console.log('Selecting all (Ctrl+A)...');
    await page.keyboard.down('Control');
    await page.keyboard.press('a');
    await page.keyboard.up('Control');
    await page.waitForTimeout(500);

    console.log('Copying (Ctrl+C)...');
    await page.keyboard.down('Control');
    await page.keyboard.press('c');
    await page.keyboard.up('Control');
    await page.waitForTimeout(1000);

    // Read the clipboard from inside the container via wl-paste (Wayland native)
    console.log('Reading clipboard from container...');
    let clipboardContent = '';
    try {
      clipboardContent = execSync(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} wl-paste`).toString();
    } catch (e) {
      throw new Error('Failed to retrieve clipboard content from container (wl-paste failed)');
    }

    console.log(`Expected : "${complexString}"`);
    console.log(`Actual   : "${clipboardContent}"`);
    
    expect(clipboardContent.trim()).toBe(complexString);
  });
});
