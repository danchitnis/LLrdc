import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

test('macOS split architecture supports dynamic HDPI scaling', async ({ page }) => {
    test.setTimeout(30000);
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set viewport to avoid huge resizes BEFORE navigation
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

    // 3. Open config menu and reset HDPI to 100 first to guarantee initial state
    await page.click('#config-btn');
    const hdpiSelect = page.locator('#hdpi-select');
    await hdpiSelect.selectOption('100');
    await page.waitForTimeout(1000);

    // 4. Change HDPI to 200%
    console.log("Changing HDPI to 200%...");
    await hdpiSelect.selectOption('200');

    // 5. Verify the server applied the new HDPI scaling by checking agent logs
    const CONTAINER_NAME = 'llrdc-macos';
    
    await expect.poll(() => {
        try {
            return execSync(`docker logs ${CONTAINER_NAME}`).toString();
        } catch (e) {
            return '';
        }
    }, {
        message: 'Agent should apply dynamic 200% HDPI scaling',
        timeout: 15000,
    }).toContain('Agent received HDPI config: 200%');

    console.log("Agent received HDPI config!");

    await expect.poll(() => {
        try {
            return execSync(`docker logs ${CONTAINER_NAME}`).toString();
        } catch (e) {
            return '';
        }
    }, {
        message: 'Compositor should apply 2x scaling',
        timeout: 15000,
    }).toContain('with scale 2.000000');

    console.log("Wayland scale 2.0 applied!");

    // 6. Verify native Wayland scale via wlr-randr inside the container
    await expect.poll(() => {
        try {
            const cmd = `docker exec -u remote ${CONTAINER_NAME} bash -c 'export WAYLAND_DISPLAY=wayland-0; export XDG_RUNTIME_DIR=/tmp/llrdc-run; wlr-randr --output HEADLESS-1'`;
            return execSync(cmd).toString().trim();
        } catch (e) {
            return '';
        }
    }, {
        message: 'Native Wayland scale should be 2.000000',
        timeout: 15000,
    }).toContain('Scale: 2.000000');

    console.log("Confirmed 2x scale via wlr-randr!");

    // 7. Ensure display recovers and is rendering
    await page.waitForFunction(() => {
        const canvas = document.getElementById('display') as HTMLCanvasElement;
        const client = (window as any).__llrdcClient;
        return canvas && canvas.width > 0 && client && client.getState().totalDecoded > 0;
    }, { timeout: 15000 });

    console.log("Video stream recovered after HDPI change!");
});
