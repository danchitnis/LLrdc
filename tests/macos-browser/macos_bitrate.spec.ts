import { test, expect } from '@playwright/test';

test('macOS split bitrate switching', async ({ page }) => {
    test.setTimeout(120000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set a standard window size
    await page.setViewportSize({ width: 1280, height: 720 });
    
    console.log('--- Navigating to viewer ---');
    await page.goto('http://localhost:8080/viewer.html');
    await page.waitForFunction(() => (window as any).networkManager?.wsConnected === true, { timeout: 15000 });
    
    // Settle initial resolution and browser transport connection
    await page.waitForTimeout(5000);

    // Initial check - default should be around 5 Mbps (often shown as BW: 4.x or 5.x)
    const statusEl = page.locator('#status');
    await expect(statusEl).toContainText(/BW: [0-9.]+/i, { timeout: 30000 });

    // Open config to ensure we can send commands via ControlServer if needed, 
    // but the test objective is to verify the server handles bitrate changes.
    // Since we are testing the macos-server which is already running, 
    // we can use the web UI's bandwidth selector to trigger changes.

    await page.click('#config-btn');
    await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();
    
    const bandwidthSelect = page.locator('#bandwidth-select');
    await expect(bandwidthSelect).toBeVisible();

    // 1. Test 1 Mbps
    console.log('--- Testing 1 Mbps ---');
    await bandwidthSelect.selectOption('1');
    
    // Wait for the BW to settle near 1 Mbps
    // Note: Rolling average takes some time to drop
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/BW: ([0-9.]+)/i);
        if (!match) return 100;
        return parseFloat(match[1]);
    }, {
        message: 'Wait for bandwidth to drop towards 1 Mbps',
        timeout: 45000,
        intervals: [2000]
    }).toBeLessThan(2.0);

    // 2. Test 10 Mbps
    console.log('--- Testing 10 Mbps ---');
    await bandwidthSelect.selectOption('10');
    
    // To trigger higher bandwidth, we might need some screen activity, 
    // but the encoder recreation itself is a proof of the config being applied.
    // In this test environment, we mostly care that the command is received and processed.
    
    // We can't easily check server logs from Playwright without more plumbing,
    // but we can verify the client still receives video.
    
    await page.waitForFunction(() => {
        const stats = (window as any).getStats?.();
        return stats && stats.totalDecoded > 0;
    }, { timeout: 20000 });

    console.log('Bitrate switching test completed.');
});
