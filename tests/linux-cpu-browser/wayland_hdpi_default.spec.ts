import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from '../helpers';

const CONTAINER_NAME = 'llrdc-wayland-hdpi-default-test';
const PORT = '8093';

test.describe('Wayland HDPI Default Scaling', () => {
  test.beforeAll(async () => {
    // Ensure any dangling container from a previous failed run is removed
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }

    console.log('Starting container without explicit HDPI (should default to 0)...');
    const containerImage = process.env.CONTAINER_IMAGE || 'danchitnis/llrdc:latest';
    // Start WITHOUT --hdpi flag to ensure it defaults to 0 internally
    execSync(`IMAGE_NAME=${containerImage.split(':')[0]} IMAGE_TAG=${containerImage.split(':')[1] || 'latest'} PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
    
    await waitForServerReady(`http://localhost:${PORT}`, 60000);
  });

  test.afterEach(async ({}, testInfo) => {
    if (testInfo.status !== testInfo.expectedStatus) {
      console.log('Test failed, keeping container for inspection.');
    } else {
      console.log('Test passed, cleaning up container...');
      try {
        execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
      } catch (e) {
        // ignore
      }
    }
  });

  test('should map default HDPI 0 to 100 in the UI', async ({ page }) => {
    test.setTimeout(60000);
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    await page.setViewportSize({ width: 1280, height: 819 });
    
    // 1. Load the page
    await page.goto(`http://localhost:${PORT}`);
    
    // 2. Wait for connection
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[(WebRTC|WebTransport|WebCodecs|WebSocket)/i, { timeout: 60000 });

    // 3. Verify initial HDPI dropdown value is mapped to 100 (from 0)
    const hdpiSelect = page.locator('#hdpi-select');
    await expect(hdpiSelect).toHaveValue('100', { timeout: 10000 });
  });
});
