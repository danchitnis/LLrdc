import { test, expect, Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady, waitForStreamingFrames } from '../helpers';

const CONTAINER_NAME = 'llrdc-res-flow-test';
const PORT = '8094';

test.describe('Resolution Flow Verification', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    console.log('Stopping any existing container...');
    try { execSync(`docker stop ${CONTAINER_NAME}`, { stdio: 'ignore' }); } catch (e) {}
    try { execSync(`docker rm ${CONTAINER_NAME}`, { stdio: 'ignore' }); } catch (e) {}

    console.log('Starting container for resolution flow test...');
    execSync(`HOST_PORT=${PORT} PORT=${PORT} ./docker-run.sh --name ${CONTAINER_NAME} --detach --tag latest`, { stdio: 'inherit' });
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try { execSync(`docker stop ${CONTAINER_NAME}`, { stdio: 'ignore' }); } catch (e) {}
    try { execSync(`docker rm ${CONTAINER_NAME}`, { stdio: 'ignore' }); } catch (e) {}
  });

  const getVideoResolution = async (page: Page) => {
    return await page.evaluate(() => {
      const canvas = document.getElementById('display') as HTMLCanvasElement | null;
      return {
        width: canvas?.width ?? 0,
        height: canvas?.height ?? 0,
      };
    });
  };

  test('should verify Responsive -> 720p -> 1080p -> Responsive flow', async ({ page }) => {
    test.setTimeout(60000);
    
    // Set a specific viewport that is NOT a standard resolution
    const viewportW = 1100;
    const viewportH = 700;
    await page.setViewportSize({ width: viewportW, height: viewportH });
    
    await page.goto(`http://localhost:${PORT}`);
    
    // 1. Wait for connection
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[(WebTransport|WebCodecs|WebSocket)/i, { timeout: 30000 });

    const maxResSelect = page.locator('#max-res-select');

    // 1b. Wait for stream to start
    console.log('Waiting for stream to start...');
    await waitForStreamingFrames(page, 'Initial stream should be active', 20000);

    // 2. Initial Responsive Check
    // The server aligns to 8 pixels.
    // viewportH = 700. topBar = 48. containerH = 652. Aligned to 8 = 648.
    // viewportW = 1100. Aligned to 8 = 1096.
    console.log('Checking initial responsive resolution...');
    await expect.poll(async () => {
      const res = await getVideoResolution(page);
      console.log(`Current resolution: ${res.width}x${res.height}`);
      return res;
    }, {
      message: 'Initial resolution should match viewport (aligned minus top bar)',
      timeout: 15000,
    }).toMatchObject({
      width: 1096,
      height: 648,
    });

    // 3. Open Config Menu
    console.log('Opening config menu...');
    await page.click('#config-btn');

    // 4. Switch to 720p
    console.log('Switching to 720p...');
    await maxResSelect.selectOption('720');
    await expect.poll(() => getVideoResolution(page), {
      message: 'Resolution should switch to 1280x720',
      timeout: 15000,
    }).toMatchObject({
      width: 1280,
      height: 720,
    });

    // 4. Switch to 1080p
    console.log('Switching to 1080p...');
    await maxResSelect.selectOption('1080');
    await expect.poll(() => getVideoResolution(page), {
      message: 'Resolution should switch to 1920x1080',
      timeout: 15000,
    }).toMatchObject({
      width: 1920,
      height: 1080,
    });

    // 5. Switch back to Responsive
    console.log('Switching back to Responsive...');
    await maxResSelect.selectOption('0');
    await expect.poll(() => getVideoResolution(page), {
      message: 'Resolution should return to viewport size (aligned)',
      timeout: 15000,
    }).toMatchObject({
      width: 1096,
      height: 648,
    });

    console.log('Resolution flow verified successfully!');
  });

  test('should verify High DPI Responsive flow (DPR=2.0)', async ({ browser }) => {
    test.setTimeout(60000);
    
    // Create a new context with DPR=2
    const dprContext = await browser.newContext({
      viewport: { width: 1000, height: 600 },
      deviceScaleFactor: 2,
    });
    const dprPage = await dprContext.newPage();
    
    await dprPage.goto(`http://localhost:${PORT}`);
    
    // 1. Wait for connection
    const statusEl = dprPage.locator('#status');
      await expect(statusEl).toHaveText(/\[(WebTransport|WebCodecs|WebSocket)/i, { timeout: 30000 });

    // 1b. Wait for stream to start
    console.log('Waiting for High DPI stream to start...');
    await waitForStreamingFrames(dprPage, 'High DPI stream should be active', 20000);

    // 2. High DPI Responsive Check
    // CSS viewport 1000x600. With DPR=2, physical pixels = 2000x1200.
    // Aligned to 8: 2000x1200 is already aligned.
    // Container height is viewportH - 48 (top bar) = 552. Physical = 1104.
    // Expected: 2000x1104.
    console.log('Checking High DPI responsive resolution...');
    await expect.poll(async () => {
      const res = await getVideoResolution(dprPage);
      console.log(`Current High DPI resolution: ${res.width}x${res.height}`);
      return res;
    }, {
      message: 'Resolution should match physical pixels (viewport * DPR)',
      timeout: 15000,
    }).toMatchObject({
      width: 2000,
      height: 1104,
    });

    console.log('High DPI Resolution flow verified successfully!');
    await dprPage.close();
  });
});
