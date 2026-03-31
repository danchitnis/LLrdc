import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-hdpi-test';
const PORT = '8092';

test.describe('Wayland HDPI Scaling', () => {
  test.beforeAll(async () => {
    // Ensure any dangling container from a previous failed run is removed
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {
      // ignore
    }

    console.log('Starting container with HDPI=200...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080/tcp -p ${PORT}:8080/udp -e PORT=8080 -e WEBRTC_PUBLIC_IP=127.0.0.1 -e HDPI=200 danchitnis/llrdc:latest`);
    
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

  test.afterAll(async () => {
    // No-op to avoid double cleanup
  });

  test('should verify initial HDPI and change it dynamically', async ({ page }) => {
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    
    // 1. Load the page
    await page.goto(`http://localhost:${PORT}`);
    
    // 2. Wait for WebRTC connection
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[WebRTC/i, { timeout: 30000 });

    // 3. Verify the video stream is actually playing and receiving frames
    await expect.poll(async () => {
      return await page.evaluate(() => {
        const video = document.querySelector('video');
        return video && video.currentTime > 0 && !video.paused && video.readyState > 2;
      });
    }, {
      message: 'Video should be actively playing and receiving WebRTC frames',
      timeout: 10000,
    }).toBeTruthy();

    // 4. Verify initial HDPI dropdown value is 200
    const hdpiSelect = page.locator('#hdpi-select');
    await expect(hdpiSelect).toHaveValue('200', { timeout: 10000 });

    // 5. Open config menu
    await page.click('#config-btn');
    
    // 6. Change HDPI to 150%
    console.log('Changing HDPI to 150%...');
    await hdpiSelect.selectOption('150');

    // 7. Verify that the server applied the new HDPI scaling by checking logs
    await expect.poll(() => {
      try {
        return execSync(`docker logs ${CONTAINER_NAME}`).toString();
      } catch (e) {
        return '';
      }
    }, {
      message: 'Server should apply dynamic 150% HDPI scaling',
      timeout: 20000,
    }).toContain('Applying HDPI scaling: 150% (DPI: 144)');

    console.log('HDPI change verified in server logs.');

    // 8. Explicitly test that Wayland Native Scaling has been applied correctly for HDPI=150
    const queryWlrRandr = () => {
      try {
        const cmd = `docker exec -u remote ${CONTAINER_NAME} bash -c 'export WAYLAND_DISPLAY=wayland-0; export XDG_RUNTIME_DIR=/tmp/llrdc-run; wlr-randr --output HEADLESS-1'`;
        return execSync(cmd).toString().trim();
      } catch (e) {
        return '';
      }
    };

    // For HDPI=150: scale should be 1.500000
    await expect.poll(() => queryWlrRandr(), { timeout: 10000 }).toContain('Scale: 1.500000');
    console.log('Native Wayland scale verified successfully.');

    // 9. Verify the video stream successfully recovered and is STILL playing after the HDPI scale restart
    await expect.poll(async () => {
      return await page.evaluate(() => {
        const video = document.querySelector('video');
        return video && video.currentTime > 0 && !video.paused && video.readyState > 2;
      });
    }, {
      message: 'Video should successfully resume playing after dynamic Wayland scale resizing',
      timeout: 10000,
    }).toBeTruthy();
    
    console.log('Stream recovery verified successfully.');
  });
});
