import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady, waitForStreamingFrames } from '../helpers';

const CONTAINER_NAME = 'llrdc-wayland-test';
const PORT = '8081';
const WT_PORT = '8091';

test.use({ ignoreHTTPSErrors: true });

test.describe('CPU browser connection smoke test', () => {
  test.setTimeout(120000);

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

  test('uses installed headed Chrome and receives CPU-streamed frames', async ({ page }) => {
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    await page.setViewportSize({ width: 1280, height: 819 });

    await expect.poll(() => fetchReadyz(`http://localhost:${PORT}`), {
      timeout: 30000,
      message: 'Wait for the CPU server to report compatibility-mode readiness',
    }).toMatchObject({
      ready: true,
      acceleratorMode: 'cpu',
      useIntel: false,
      directBuffer: {
        captureMode: 'compat',
        active: false,
      },
    });

    const cdp = await page.context().newCDPSession(page);
    const browserVersion = await cdp.send('Browser.getVersion');
    console.log(`Browser product: ${browserVersion.product}`);
    console.log(`Configured executable: ${process.env.PLAYWRIGHT_CHROME_EXECUTABLE || 'Chrome channel'}`);
    expect(browserVersion.product).toMatch(/^Chrome\//);
    expect(browserVersion.userAgent).not.toContain('HeadlessChrome');

    console.log(`Navigating to https://localhost:${WT_PORT}...`);
    await page.goto(`https://localhost:${WT_PORT}`);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebTransport/i, { timeout: 20000 });
    await waitForStreamingFrames(page, 'Wait for decoded CPU-streamed frames', 30000);
    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 10000,
      message: 'Wait for the server-side WebTransport session log',
    }).toContain('WebTransport session established');

    const finalStatus = await statusEl.textContent();
    console.log(`Final Status: ${finalStatus}`);
  });
});
