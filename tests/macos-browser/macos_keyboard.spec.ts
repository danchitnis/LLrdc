import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady, waitForStreamingFrames } from '../helpers';

const CONTAINER_NAME = 'llrdc-macos';
const BASE_URL = 'http://127.0.0.1:8080';

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

test('macOS split architecture keyboard: professional mousepad verification', async ({ page }) => {
    test.setTimeout(60000);
    
    console.log('Waiting for server...');
    await waitForServerReady(BASE_URL, 15000);
    await page.goto(`${BASE_URL}/viewer.html`);
    await waitForStreamingFrames(page, "Stream did not start");

    const TEST_FILE = '/tmp/kb_robust_mousepad.txt';
    run(`docker exec -u remote ${CONTAINER_NAME} rm -f ${TEST_FILE}`);

    // Ensure wlrctl is installed
    console.log('Installing wlrctl...');
    run(`docker exec -u root ${CONTAINER_NAME} apt-get update && docker exec -u root ${CONTAINER_NAME} apt-get install -y wlrctl`);

    console.log('Spawning Mousepad (forcing Wayland backend)...');
    // Force Wayland backend for mousepad so wlrctl can see it
    run(`docker exec -u remote -d -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run -e GDK_BACKEND=wayland ${CONTAINER_NAME} mousepad ${TEST_FILE}`);
    
    // Wait and focus
    let ready = false;
    for (let i = 0; i < 20; i++) {
        const list = run(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} wlrctl toplevel list || true`);
        console.log(`Window list attempt ${i}: ${list}`);
        if (list.length > 0) {
            console.log('Window detected, maximizing and focusing...');
            run(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} wlrctl toplevel maximize`);
            run(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} wlrctl toplevel focus`);
            ready = true;
            break;
        }
        await page.waitForTimeout(1000);
    }
    expect(ready).toBe(true);

    // Click display center (now that it's maximized) to ensure browser focus
    await page.locator('#display-container').click({ position: { x: 400, y: 300 } });
    await page.waitForTimeout(500);

    // 1. Normal
    console.log('Typing: hello');
    await page.keyboard.type('hello', { delay: 100 });
    
    // 2. Shift
    console.log('Typing (Shift): world');
    await page.keyboard.down('Shift');
    await page.keyboard.type('world', { delay: 100 });
    await page.keyboard.up('Shift');

    // 3. Ctrl (Save)
    console.log('Sending Ctrl+S to save');
    await page.keyboard.down('Control');
    await page.keyboard.press('s', { delay: 100 });
    await page.keyboard.up('Control');

    await page.waitForTimeout(2000);

    // 4. Ctrl (Quit) - ensure file is flushed
    console.log('Sending Ctrl+Q to quit');
    await page.keyboard.down('Control');
    await page.keyboard.press('q', { delay: 100 });
    await page.keyboard.up('Control');

    await page.waitForTimeout(2000);

    console.log('Verifying content...');
    const content = run(`docker exec -u remote ${CONTAINER_NAME} cat ${TEST_FILE}`).trim();
    
    const expected = 'helloWORLD';
    console.log(`Expected: ${expected}`);
    console.log(`Got:      ${content}`);
    
    expect(content).toBe(expected);
});
