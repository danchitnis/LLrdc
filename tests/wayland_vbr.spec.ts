import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-vbr-test';
const PORT = '8095';

test.describe('Wayland VBR and Damage Tracking E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(60000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for Wayland VBR/Damage Tracking test...');
    execSync(`PORT=${PORT} VBR=true DAMAGE_TRACKING=true ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
    
    await waitForServerReady(`http://localhost:${PORT}`);
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should toggle damage tracking and VBR independently', async ({ page }) => {
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    await page.setViewportSize({ width: 1280, height: 819 });
    await page.goto(`http://localhost:${PORT}`);

    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });

    // Open config menu
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    const qualityTabLocator = page.locator('.config-tab-btn[data-tab="tab-quality"]');
    await qualityTabLocator.click();

    // Verify it started with Damage Tracking enabled by default (no -D flag in wf-recorder)
    let logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
    
    // Find the latest wf-recorder launch in logs
    let wfRecorderLogs = logs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
    expect(wfRecorderLogs.length).toBeGreaterThan(0);
    let latestWfRecorderLog = wfRecorderLogs[wfRecorderLogs.length - 1];
    expect(latestWfRecorderLog).not.toContain('-D');

    // --- TEST DAMAGE TRACKING ---
    // Select the Damage Tracking checkbox (uncheck it)
    console.log('Disabling Damage Tracking...');
    const dtCheckbox = page.locator('#damage-tracking-checkbox');
    await dtCheckbox.click();
    await expect(dtCheckbox).not.toBeChecked();
    await page.waitForTimeout(2000);

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 30000,
    }).toContain('Received Damage Tracking config: false');
    
    await expect.poll(() => {
        try {
            const currentLogs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            const recorderLines = currentLogs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
            return recorderLines[recorderLines.length - 1] || '';
        } catch (e) {
            return '';
        }
    }, {
        message: 'wf-recorder should restart with -D flag',
        timeout: 10000,
    }).toContain('-D');

    // --- TEST VBR ---
    console.log('Disabling VBR...');
    const vbrCheckbox = page.locator('#vbr-checkbox');
    await vbrCheckbox.click();
    await expect(vbrCheckbox).not.toBeChecked();
    await page.waitForTimeout(2000);

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
      timeout: 30000,
    }).toContain('Received VBR config: false');
    
    // wf-recorder should STILL have -D because Damage Tracking is still false
    await expect.poll(() => {
        try {
            const currentLogs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            const recorderLines = currentLogs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
            return recorderLines[recorderLines.length - 1] || '';
        } catch (e) {
            return '';
        }
    }, {
        message: 'wf-recorder should still restart with -D flag',
        timeout: 10000,
    }).toContain('-D');

    // --- RE-ENABLE DAMAGE TRACKING ---
    console.log('Enabling Damage Tracking...');
    await dtCheckbox.click();
    await expect(dtCheckbox).toBeChecked();
    await page.waitForTimeout(2000);

    await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
        timeout: 30000,
    }).toContain('Received Damage Tracking config: true');
    
    await expect.poll(() => {
        try {
            const currentLogs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            const recorderLines = currentLogs.split('\n').filter(line => line.includes('Starting wf-recorder capture:'));
            return recorderLines[recorderLines.length - 1] || '-D'; // Default to -D to keep polling if empty
        } catch (e) {
            return '-D';
        }
    }, {
        message: 'wf-recorder should restart WITHOUT -D flag',
        timeout: 10000,
    }).not.toContain('-D');

    // Verify it still says WebRTC
    await expect(statusEl).toHaveText(/WebRTC/i);
  });
});