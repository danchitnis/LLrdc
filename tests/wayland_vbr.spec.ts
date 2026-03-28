import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-wayland-vbr-test';
const PORT = '8085';

test.describe('Wayland VBR E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for Wayland VBR test...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 -e USE_WAYLAND=true danchitnis/llrdc:wayland-latest`);
    
    await new Promise(r => setTimeout(r, 30000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should toggle damage tracking based on VBR config', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    // Open config menu
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    const qualityTabLocator = page.locator('.config-tab-btn[data-tab="tab-quality"]');
    await qualityTabLocator.click();

    // Verify it started with VBR enabled by default (no -D flag in wf-recorder)
    let logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    
    // Find the latest wf-recorder launch in logs
    let wfRecorderLogs = logs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
    expect(wfRecorderLogs.length).toBeGreaterThan(0);
    let latestWfRecorderLog = wfRecorderLogs[wfRecorderLogs.length - 1];
    expect(latestWfRecorderLog).not.toContain('-D');

    // Select the VBR checkbox (uncheck it)
    console.log('Disabling VBR...');
    const vbrCheckbox = page.locator('#vbr-checkbox');
    await vbrCheckbox.uncheck();

    // Wait for propagation and ffmpeg restart
    await page.waitForTimeout(5000);

    logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    expect(logs).toContain('Received VBR config: false');

    wfRecorderLogs = logs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
    latestWfRecorderLog = wfRecorderLogs[wfRecorderLogs.length - 1];
    expect(latestWfRecorderLog).toContain('-D');

    // Re-enable VBR
    console.log('Enabling VBR...');
    await vbrCheckbox.check();

    await page.waitForTimeout(5000);

    logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    expect(logs).toContain('Received VBR config: true');

    wfRecorderLogs = logs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
    latestWfRecorderLog = wfRecorderLogs[wfRecorderLogs.length - 1];
    expect(latestWfRecorderLog).not.toContain('-D');

    // Verify it still says WebRTC
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});
