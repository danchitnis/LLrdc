import { test, expect } from '@playwright/test';

test('macOS split architecture supports dynamic resolution switching', async ({ page }) => {
    test.setTimeout(90000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // 1. Navigate to the local macOS server
    await page.goto('http://localhost:8080/viewer.html');

    // 2. Wait for the video element and ensure it's playing
    const video = page.locator('#webrtc-video');
    await expect(video).toBeAttached({ timeout: 15000 });

    await page.waitForFunction(() => {
        const vid = document.getElementById('webrtc-video') as HTMLVideoElement;
        return vid && vid.readyState >= 3 && !vid.paused && vid.currentTime > 0;
    }, { timeout: 20000 });

    // Capture initial resolution (default should be 1080p if not changed)
    let dims = await video.evaluate((v: HTMLVideoElement) => ({ w: v.videoWidth, h: v.videoHeight }));
    console.log(`Initial video resolution: ${dims.w}x${dims.h}`);
    
    // 3. Switch to 720p
    console.log("Switching to 720p...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '720';
            (window as any).sendConfig();
        } else {
            // Fallback if UI is missing
            const config = (window as any).buildConfigMessage();
            config.max_res = 720;
            (window as any).webrtcSession.sendConfig(config);
        }
    });

    // 4. Wait for resolution change
    await page.waitForFunction(() => {
        const vid = document.getElementById('webrtc-video') as HTMLVideoElement;
        return vid && vid.videoWidth === 1280 && vid.videoHeight === 720;
    }, { timeout: 30000 });

    dims = await video.evaluate((v: HTMLVideoElement) => ({ w: v.videoWidth, h: v.videoHeight }));
    expect(dims.w).toBe(1280);
    expect(dims.h).toBe(720);
    console.log("Successfully switched to 720p!");

    // 5. Switch back to 1080p
    console.log("Switching to 1080p...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '1080';
            (window as any).sendConfig();
        } else {
            const config = (window as any).buildConfigMessage();
            config.max_res = 1080;
            (window as any).webrtcSession.sendConfig(config);
        }
    });

    await page.waitForFunction(() => {
        const vid = document.getElementById('webrtc-video') as HTMLVideoElement;
        return vid && vid.videoWidth === 1920 && vid.videoHeight === 1080;
    }, { timeout: 30000 });

    dims = await video.evaluate((v: HTMLVideoElement) => ({ w: v.videoWidth, h: v.videoHeight }));
    expect(dims.w).toBe(1920);
    expect(dims.h).toBe(1080);
    console.log("Successfully switched to 1080p!");

    // 6. Test Responsive (Adaptive) resolution
    console.log("Switching to Responsive mode...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '0';
            (window as any).sendConfig();
        } else {
            const config = (window as any).buildConfigMessage();
            config.max_res = 0;
            (window as any).webrtcSession.sendConfig(config);
        }
    });
    
    // Resize the viewport to something custom
    const customWidth = 1000;
    const customHeight = 600;
    // Note: server clamps and aligns to 8 pixels, so 1000x600 should become 1000x600 if it fits
    // (1000 is divisible by 8? 1000/8 = 125. Yes. 600/8 = 75. Yes.)
    
    await page.setViewportSize({ width: customWidth, height: customHeight + 100 }); // +100 for UI overhead
    
    // The client sends resize messages when the window resizes in responsive mode.
    // We wait for the video to match the target resolution (or close to it due to aspect ratio/clamping)
    await page.waitForFunction((target) => {
        const vid = document.getElementById('webrtc-video') as HTMLVideoElement;
        // In responsive mode, the video might be slightly smaller than viewport due to UI
        return vid && vid.videoWidth > 800 && vid.videoWidth <= 1000;
    }, { width: customWidth, height: customHeight }, { timeout: 30000 });

    dims = await video.evaluate((v: HTMLVideoElement) => ({ w: v.videoWidth, h: v.videoHeight }));
    console.log(`Responsive video resolution: ${dims.w}x${dims.h}`);
    expect(dims.w).toBeGreaterThan(800);
});
