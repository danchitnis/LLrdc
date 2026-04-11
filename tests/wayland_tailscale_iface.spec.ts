import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-tailscale-test';
const PORT = '8083';
const SERVER_URL = `http://localhost:${PORT}`;

function cleanupContainer() {
  try {
    execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
  } catch {
    // ignore cleanup failures
  }
}

test.describe('Wayland WebRTC with Tailscale Interface Selection', () => {
  test.beforeAll(async () => {
    cleanupContainer();

    execSync('ip link show tailscale0 >/dev/null 2>&1');

    execSync(
      `PORT=${PORT} HOST_PORT=${PORT} ./docker-run.sh --wayland -d --host-net -i tailscale0 --name ${CONTAINER_NAME}`,
      { stdio: 'inherit' }
    );

    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    cleanupContainer();
  });

  test('establishes WebRTC streaming when started with -i tailscale0', async ({ page }) => {
    page.on('console', (msg) => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));

    await page.goto(SERVER_URL);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebRTC/i, { timeout: 30000 });

    await expect.poll(async () => {
      return await page.evaluate(() => {
        const video = document.querySelector('video');
        return !!(video && video.currentTime > 0 && !video.paused && video.readyState > 2);
      });
    }, {
      message: 'Video should be actively playing over WebRTC when using -i tailscale0',
      timeout: 15000,
    }).toBeTruthy();
  });
});
