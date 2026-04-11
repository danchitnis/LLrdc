import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from '../helpers';

// Force headed mode for this test file
test.use({ headless: false });

const CONTAINER_NAME = 'llrdc-gpu-visibility-test';
const PORT = '8080';

test.describe('Hardware Options Visibility (CPU Mode)', () => {
  test.setTimeout(120000); // 2 minutes total for everything

  test.beforeAll(async () => {
    test.info().annotations.push({ type: 'info', description: 'Starting container' });
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }

    console.log(`Starting container in CPU mode on port ${PORT}...`);
    // Use the locally built image
    execSync(`./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`, {
        env: { ...process.env, IMAGE_NAME: 'danchitnis/llrdc', IMAGE_TAG: 'latest', PORT: PORT },
        stdio: 'inherit' 
    });
    
    try {
        await waitForServerReady(`http://localhost:${PORT}`, 90000); // 90s for server to start
    } catch (e) {
        console.error('Server failed to start. Logs:');
        console.error(execSync(`docker logs ${CONTAINER_NAME}`).toString());
        throw e;
    }
  });

  test.afterAll(async ({ }, testInfo) => {
    if (testInfo.status !== testInfo.expectedStatus) {
        console.log(`Test failed. Keeping container ${CONTAINER_NAME} for inspection.`);
        return;
    }
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }
  });

  test('should hide hardware-only options when running without hardware acceleration', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);
    
    await expect(page.locator('#status')).toContainText(/\[vp8\]/i, { timeout: 20000 });

    // Open Config
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    // 1. Check Codec Select for hidden GPU options
    const hardwareAvailable = await page.evaluate(() => window.hardwareAccelerationAvailable);
    console.log(`Browser reports hardwareAccelerationAvailable: ${hardwareAvailable}`);

    // Wait a bit for the UI to definitely update
    await page.waitForTimeout(1000);

    const gpuCodecOptions = page.locator('select#video-codec-select option.codec-opt-gpu');
    const count = await gpuCodecOptions.count();
    console.log(`Found ${count} GPU codec options.`);
    for (let i = 0; i < count; i++) {
        const isVisible = await gpuCodecOptions.nth(i).isVisible();
        const value = await gpuCodecOptions.nth(i).getAttribute('value');
        console.log(`Option ${value} visible: ${isVisible}`);
        await expect(gpuCodecOptions.nth(i)).not.toBeVisible();
    }

    // 2. Check Direct Buffer status (should be hidden)
    const directBufferContainer = page.locator('.config-group.hardware-only:has(#direct-buffer-status)');
    await expect(directBufferContainer).not.toBeVisible();

    // 3. Check Performance tab for NVENC ULL checkbox
    await page.locator('.config-tab-btn[data-tab="tab-performance"]').click();
    const nvencUllContainer = page.locator('.config-group.nvidia-only:has(#nvenc-latency-checkbox)');
    await expect(nvencUllContainer).not.toBeVisible();

    // 4. Verify "Client Hardware Acceleration" label exists (renamed from GPU Decoding)
    await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();
    await expect(page.locator('text=Enable Client Hardware Acceleration')).toBeVisible();
    
    console.log('GPU options visibility test passed (all hidden as expected).');

    // Keep browser open for a few seconds if headed so user can see it
    await page.waitForTimeout(5000);
  });
});
