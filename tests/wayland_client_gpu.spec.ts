import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const PORT = 8123;
const CONTAINER_NAME = `llrdc-wayland-gpu-test-${PORT}`;

test.describe('Wayland Client GPU Decoding', () => {
  test.beforeAll(async () => {
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting Wayland container...');
    // Using docker-run.sh to ensure local changes are used if it builds/runs correctly
    // or just direct docker run if we assume the image is ready.
    // Given the context, ./docker-run.sh --wayland is preferred.
    execSync(`./docker-run.sh --wayland --detach --name ${CONTAINER_NAME} --hdpi 100`, {
        env: { ...process.env, HOST_PORT: PORT.toString() }
    });
    
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should toggle client GPU decoding and verify hardware acceleration', async ({ page }) => {
    test.setTimeout(90000);
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    
    await page.goto(`http://localhost:${PORT}`);
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebRTC|WebCodecs/i, { timeout: 45000 });

    // Wait for at least one frame to be decoded
    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).getStats()?.totalDecoded);
    }, { 
        message: 'Wait for initial frames to be decoded',
        timeout: 45000 
    }).toBeGreaterThan(0);

    // Inject spy on VideoDecoder.configure
    await page.evaluate(() => {
        // @ts-ignore
        window.lastDecoderConfig = null;
        const originalConfigure = VideoDecoder.prototype.configure;
        VideoDecoder.prototype.configure = function(config) {
            // @ts-ignore
            window.lastDecoderConfig = config;
            return originalConfigure.call(this, config);
        };
    });

    // Open config and go to Stream tab (default)
    await page.locator('#config-btn').click();
    const clientGpuCheckbox = page.locator('#client-gpu-checkbox');

    // 1. Verify default is software (unchecked)
    // Note: If the browser supports it, it might have already initialized.
    // Let's trigger a toggle to be sure.

    // 2. Enable Client GPU Decoding
    await clientGpuCheckbox.check();
    
    // Verify hardware acceleration is preferred
    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).lastDecoderConfig?.hardwareAcceleration);
    }, { 
        message: 'Should prefer hardware acceleration when checked',
        timeout: 15000 
    }).toBe('prefer-hardware');

    // Wait for it to start decoding again (might reset to 0)
    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).getStats().totalDecoded);
    }, { 
        message: 'Wait for decoding to resume after enabling GPU',
        timeout: 15000 
    }).toBeGreaterThan(0);

    // 3. Disable Client GPU Decoding
    await clientGpuCheckbox.uncheck();

    // Verify software acceleration is preferred
    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).lastDecoderConfig?.hardwareAcceleration);
    }, { 
        message: 'Should prefer software acceleration when unchecked',
        timeout: 15000 
    }).toBe('prefer-software');

    // Wait for it to start decoding again
    await expect.poll(async () => {
        return await page.evaluate(() => (window as any).getStats().totalDecoded);
    }, { 
        message: 'Wait for decoding to resume after disabling GPU',
        timeout: 15000 
    }).toBeGreaterThan(0);
  });
});
