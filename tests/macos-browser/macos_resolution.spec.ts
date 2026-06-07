import { test, expect } from '@playwright/test';

test('macOS split architecture supports dynamic resolution switching', async ({ page }) => {
    test.setTimeout(90000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set a deterministic viewport to prevent state leakage and huge resizes
    await page.setViewportSize({ width: 1280, height: 720 });

    // 1. Navigate to the local macOS server
    await page.goto('http://localhost:8080/viewer.html');

    // 2. Wait for the display canvas element and ensure it's rendering
    const display = page.locator('#display');
    await expect(display).toBeAttached({ timeout: 15000 });

    // Reset HDPI in case of pollution from prior tests
    console.log("Resetting HDPI to clean state...");
    await page.click('#config-btn');
    const hdpiSelect = page.locator('#hdpi-select');
    await hdpiSelect.selectOption('100');
    await page.click('#config-btn');

    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        return canvas && canvas.width > 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    // Capture initial resolution (default should be 1080p if not changed)
    let dims = await display.evaluate((canvas: HTMLCanvasElement) => ({ w: canvas.width, h: canvas.height }));
    console.log(`Initial display resolution: ${dims.w}x${dims.h}`);
    
    // 3. Switch to 720p
    console.log("Switching to 720p...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '720';
            select.dispatchEvent(new Event('change'));
        } else {
            // Fallback if UI is missing
            const client = (window as any).__llrdcClient;
            if (client) {
                client.sendConfig({ type: 'config', max_res: 720 });
            }
        }
    });

    // 4. Wait for resolution change
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        return canvas && canvas.width === 1280 && canvas.height === 720;
    }, { timeout: 15000 });

    dims = await display.evaluate((canvas: HTMLCanvasElement) => ({ w: canvas.width, h: canvas.height }));
    expect(dims.w).toBe(1280);
    expect(dims.h).toBe(720);
    console.log("Successfully switched to 720p!");

    // 5. Switch back to 1080p
    console.log("Switching to 1080p...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '1080';
            select.dispatchEvent(new Event('change'));
        } else {
            const client = (window as any).__llrdcClient;
            if (client) {
                client.sendConfig({ type: 'config', max_res: 1080 });
            }
        }
    });

    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        return canvas && canvas.width === 1920 && canvas.height === 1080;
    }, { timeout: 15000 });

    dims = await display.evaluate((canvas: HTMLCanvasElement) => ({ w: canvas.width, h: canvas.height }));
    expect(dims.w).toBe(1920);
    expect(dims.h).toBe(1080);
    console.log("Successfully switched to 1080p!");

    // 6. Test Responsive (Adaptive) resolution
    console.log("Switching to Responsive mode...");
    await page.evaluate(() => {
        const select = document.getElementById('max-res-select') as HTMLSelectElement;
        if (select) {
            select.value = '0';
            select.dispatchEvent(new Event('change'));
        } else {
            const client = (window as any).__llrdcClient;
            if (client) {
                client.sendConfig({ type: 'config', max_res: 0 });
            }
        }
    });
    
    // Resize the viewport to something custom
    const customWidth = 1000;
    const customHeight = 600;
    
    await page.setViewportSize({ width: customWidth, height: customHeight + 100 }); // +100 for UI overhead
    
    // The client sends resize messages when the window resizes in responsive mode.
    // We wait for the display to match the target resolution (or close to it due to aspect ratio/clamping)
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        // In responsive mode, the display might be slightly smaller than viewport due to UI
        return canvas && canvas.width > 800 && canvas.width <= 1000;
    }, { timeout: 15000 });

    dims = await display.evaluate((canvas: HTMLCanvasElement) => ({ w: canvas.width, h: canvas.height }));
    console.log(`Responsive display resolution: ${dims.w}x${dims.h}`);
    expect(dims.w).toBeGreaterThan(800);
});
