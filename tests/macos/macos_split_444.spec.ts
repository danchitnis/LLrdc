import { test, expect } from '@playwright/test';

test('macOS split 4:4:4 profile switching (H.264 and HEVC)', async ({ page }) => {
    test.setTimeout(180000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set a standard window size that results in a stable Wayland resolution
    await page.setViewportSize({ width: 1280, height: 720 });
    
    // 1. Test Standard H.264 (CPU)
    console.log('--- Testing Standard H.264 (CPU) ---');
    await page.goto('http://localhost:8080/viewer.html');
    await page.waitForFunction(() => (window as any).networkManager?.wsConnected === true, { timeout: 15000 });
    
    // Settle initial resolution and WebRTC connection
    await page.waitForTimeout(5000);

    await page.click('#config-btn');
    const codecSelect = page.locator('#video-codec-select');
    await expect(codecSelect).toBeVisible();
    
    // Select H.264 (CPU)
    await codecSelect.selectOption('h264');
    
    // Wait for video to be playing and decode at least some frames
    await page.waitForFunction(() => {
        const stats = (window as any).getStats?.();
        return stats && stats.totalDecoded > 20;
    }, { timeout: 40000 });

    let frameStats1 = await getDecodedFrames(page);
    console.log(`H.264 CPU decoded frames: ${frameStats1}`);
    expect(frameStats1).toBeGreaterThan(20);

    // 2. Test H.264 (4:4:4 Screen Profile)
    console.log('--- Testing H.264 (4:4:4 Screen Profile) ---');
    
    // Select H.264 (4:4:4 Screen Profile)
    await codecSelect.selectOption('h264-444');

    // Wait for the stream to restart and decode NEW frames
    console.log('Waiting for H.264 4:4:4 session to reconnect and progress...');
    await page.waitForFunction((prev) => {
        const stats = (window as any).getStats?.();
        // Check for progression beyond previous value
        return stats && stats.totalDecoded > prev + 10;
    }, frameStats1, { timeout: 60000 });

    let frameStats2 = await getDecodedFrames(page);
    console.log(`H.264 4:4:4 final frames: ${frameStats2}`);
    expect(frameStats2).toBeGreaterThan(frameStats1);
    
    console.log(`Verified H.264 4:4:4 stream successfully.`);

    // 3. Test HEVC (4:4:4 Screen Profile)
    console.log('--- Testing HEVC (4:4:4 Screen Profile) ---');
    
    // Select HEVC (4:4:4 Screen Profile)
    await codecSelect.selectOption('h265-444');

    // Wait for the stream to restart and decode NEW frames
    console.log('Waiting for HEVC 4:4:4 session to reconnect and progress...');
    await page.waitForFunction((prev) => {
        const stats = (window as any).getStats?.();
        // Check for progression beyond previous value
        return stats && stats.totalDecoded > prev + 10;
    }, frameStats2, { timeout: 60000 });

    let frameStats3 = await getDecodedFrames(page);
    console.log(`HEVC 4:4:4 final frames: ${frameStats3}`);
    expect(frameStats3).toBeGreaterThan(frameStats2);
    
    console.log(`Verified HEVC 4:4:4 stream successfully.`);
});

async function getDecodedFrames(page: any): Promise<number> {
    return await page.evaluate(async () => {
        const stats = (window as any).getStats?.();
        return stats ? stats.totalDecoded : 0;
    });
}
