import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

test('macOS split architecture FPS switch 30 to 60 and monitor PLI', async ({ page }) => {
    test.setTimeout(120000); 
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set viewport to a stable size
    await page.setViewportSize({ width: 1280, height: 720 });
    
    // 1. Navigate to the local macOS server
    await page.goto('http://localhost:8080/viewer.html');

    // 2. Wait for the display canvas and ensure it's rendering
    const display = page.locator('#display');
    await expect(display).toBeAttached({ timeout: 15000 });

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

    // 5. Change FPS to 60
    console.log("Changing FPS to 60...");
    await framerateSelect.selectOption('60');

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
        message: 'Agent should apply dynamic 60 FPS setting',
        timeout: 15000,
    }).toContain('Agent received FPS config: 60');

    console.log("Agent received FPS config!");

    await expect.poll(() => {
        try {
            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            return logs;
        } catch (e) {
            return '';
        }
    }, {
        message: 'Compositor should apply 60Hz refresh rate',
        timeout: 15000,
    }).toContain('@ 60');

    console.log("Wayland refresh rate 60Hz applied!");

    // 7. Ensure display recovers and is rendering
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        return canvas && canvas.width > 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    console.log("Video stream recovered after FPS change! Waiting 30s to monitor PLI...");

    // 8. Wait for 30 seconds to observe if PLI messages appear
    await page.waitForTimeout(30000);

    console.log("Wait complete. Checking macos-server.log for PLI messages...");
    
    try {
        const serverLogs = execSync('cat test-logs/macos-server.log').toString();
        if (serverLogs.includes('Received PLI on video track')) {
            console.log("found PLI messages in macos-server.log");
            const pliMatches = serverLogs.match(/Received PLI on video track/g);
            console.log(`Number of PLI messages: ${pliMatches ? pliMatches.length : 0}`);
        } else {
            console.log("No PLI messages found in macos-server.log");
        }
    } catch (e) {
        console.error("Failed to read macos-server.log:", e);
    }
});
