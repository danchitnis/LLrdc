import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

test('macOS split architecture supports dynamic FPS switching', async ({ page }) => {
    test.setTimeout(90000); 
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set viewport to a stable size
    await page.setViewportSize({ width: 1280, height: 720 });
    
    // 1. Navigate to the local macOS server
    await page.goto('http://localhost:8080/viewer.html');

    // 2. Wait for the display canvas and ensure it's rendering
    const display = page.locator('#display');
    await expect(display).toBeAttached({ timeout: 15000 });

    // Wait for the display to actually have a stream and be rendering.
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        return canvas && canvas.width > 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    console.log("Video playing and stable. Opening config dropdown...");

    // 3. Open config dropdown
    const configBtn = page.locator('#config-btn');
    await configBtn.click();

    // 4. Ensure the framerate select is present
    const framerateSelect = page.locator('#framerate-select');
    await expect(framerateSelect).toBeVisible({ timeout: 15000 });

    // 5. Change FPS to 15
    console.log("Changing FPS to 15...");
    await framerateSelect.selectOption('15');

    // 6. Verify the server applied the new FPS by checking agent logs
    const CONTAINER_NAME = 'llrdc-macos';
    
    await expect.poll(() => {
        try {
            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            return logs;
        } catch (e) {
            return '';
        }
    }, {
        message: 'Agent should apply dynamic 15 FPS setting',
        timeout: 15000,
    }).toContain('Agent received FPS config: 15');

    console.log("Agent received FPS config!");

    await expect.poll(() => {
        try {
            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            return logs;
        } catch (e) {
            return '';
        }
    }, {
        message: 'Compositor should apply 15Hz refresh rate',
        timeout: 15000,
    }).toContain('@ 15');

    console.log("Wayland refresh rate 15Hz applied!");

    // 7. Ensure display recovers and is rendering
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        return canvas && canvas.width > 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    console.log("Video stream recovered after FPS change!");
});
