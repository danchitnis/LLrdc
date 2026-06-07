import { test, expect } from '@playwright/test';

test('macOS split architecture supports robust window resizing', async ({ page }) => {
    test.setTimeout(60000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // 1. Initial stable state
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.goto('http://localhost:8080/viewer.html');

    const display = page.locator('#display');
    await expect(display).toBeAttached({ timeout: 15000 });

    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        // canvas height/width should be non-zero and aligned to 8 pixels, with at least some decoded frames
        return canvas && canvas.width > 0 && canvas.height > 0 && canvas.width % 8 === 0 && canvas.height % 8 === 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    const initialW = await page.evaluate(() => (document.getElementById('display') as HTMLCanvasElement).width);
    const initialH = await page.evaluate(() => (document.getElementById('display') as HTMLCanvasElement).height);
    console.log(`Initial stream stable at ${initialW}x${initialH}.`);

    // 2. Resize to custom resolution 1
    console.log("Resizing window to 1366x768...");
    await page.setViewportSize({ width: 1366, height: 768 });

    // Wait for the display to recover and have new dimensions (different from initial)
    await expect.poll(async () => {
        return await page.evaluate(() => {
            const canvas = document.getElementById('display') as HTMLCanvasElement;
            const client = (window as any).__llrdcClient;
            return {
                w: canvas.width,
                h: canvas.height,
                ready: canvas.width > 0 && canvas.height > 0,
                decoded: client ? client.getState().totalDecoded : 0
            };
        });
    }, {
        message: 'Stream should recover with 8-pixel aligned dimensions after resize to 1366x768',
        timeout: 15000,
    }).toMatchObject({
        w: expect.any(Number),
        h: expect.any(Number),
        ready: true,
        decoded: expect.any(Number),
    });

    const dims1 = await page.evaluate(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        return { w: canvas.width, h: canvas.height };
    });
    console.log(`Stream recovered at ${dims1.w}x${dims1.h}.`);
    expect(dims1.w % 8).toBe(0);
    expect(dims1.h % 8).toBe(0);

    // 3. Resize to custom resolution 2
    console.log("Resizing window to 1440x900...");
    await page.setViewportSize({ width: 1440, height: 900 });

    await expect.poll(async () => {
        const dims = await page.evaluate(() => {
            const canvas = document.getElementById('display') as HTMLCanvasElement;
            const client = (window as any).__llrdcClient;
            return {
                w: canvas.width,
                h: canvas.height,
                ready: canvas.width > 0 && canvas.height > 0,
                decoded: client ? client.getState().totalDecoded : 0
            };
        });
        // Check dimensions are different and aligned
        if (dims.w !== dims1.w && dims.w % 8 === 0 && dims.h % 8 === 0 && dims.ready && dims.decoded > 0) {
            return true;
        }
        return false;
    }, {
        message: 'Stream should recover with new 8-pixel aligned dimensions after resize to 1440x900',
        timeout: 15000,
    }).toBe(true);

    const dims2 = await page.evaluate(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        return { w: canvas.width, h: canvas.height };
    });
    console.log(`Stream recovered at ${dims2.w}x${dims2.h}.`);
    expect(dims2.w % 8).toBe(0);
    expect(dims2.h % 8).toBe(0);

    // 4. Final verification of stability
    await page.waitForTimeout(2000);
    const isPlaying = await page.evaluate(() => {
        const client = (window as any).__llrdcClient;
        return client && client.getState().totalDecoded > 0;
    });
    expect(isPlaying).toBe(true);
    console.log("Stability verified after multiple resizes.");
});
