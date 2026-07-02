import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady, waitForStreamingFrames } from '../helpers';

const CONTAINER_NAME = 'llrdc-macos';
const BASE_URL = 'http://127.0.0.1:8080';

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

test.describe('macOS Split Clipboard Sync', () => {
    test.beforeEach(async ({ context, page }) => {
        // Grant clipboard permissions for Playwright's browser context
        await context.grantPermissions(['clipboard-read', 'clipboard-write']);

        console.log('Waiting for server...');
        await waitForServerReady(BASE_URL, 15000);
        await page.goto(`${BASE_URL}/viewer.html`);
        await waitForStreamingFrames(page, "Stream did not start");
    });

    test('should paste from host browser to remote container clipboard', async ({ page }) => {
        test.setTimeout(30000);

        // Click display container to focus
        await page.locator('#display-container').click();
        await page.waitForTimeout(500);

        // Simulate a manual paste event with some unique text
        const pasteSecret = 'PASTE_SPLIT_TEST_' + Date.now();
        console.log(`Simulating host paste event: "${pasteSecret}"`);

        await page.evaluate((text) => {
            const dt = new DataTransfer();
            dt.setData('text/plain', text);
            const pasteEvent = new ClipboardEvent('paste', {
                clipboardData: dt,
                bubbles: true,
                cancelable: true
            });
            window.dispatchEvent(pasteEvent);
        }, pasteSecret);

        await page.waitForTimeout(3000);

        // Read container clipboard via wl-paste inside the container
        console.log('Querying container clipboard via wl-paste...');
        let remoteClipboard = '';
        try {
            remoteClipboard = run(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} wl-paste`).trim();
        } catch (e) {
            throw new Error(`Failed to read container clipboard: ${e}`);
        }

        console.log(`Expected : "${pasteSecret}"`);
        console.log(`Got      : "${remoteClipboard}"`);
        expect(remoteClipboard).toBe(pasteSecret);
    });

    test('should copy from remote container clipboard to host browser clipboard', async ({ page }) => {
        test.setTimeout(30000);

        // Click display container to focus
        await page.locator('#display-container').click();
        await page.waitForTimeout(500);

        // Set remote clipboard via wl-copy inside the container
        const copySecret = 'COPY_SPLIT_TEST_' + Date.now();
        console.log(`Setting container clipboard to: "${copySecret}"`);
        run(`docker exec -u remote -e WAYLAND_DISPLAY=wayland-0 -e XDG_RUNTIME_DIR=/tmp/llrdc-run ${CONTAINER_NAME} sh -c "echo -n '${copySecret}' | wl-copy"`);

        await page.waitForTimeout(1000);

        // Trigger a user gesture (click or key) to process pending clipboard
        console.log('Triggering click to process pending clipboard...');
        await page.locator('#display-container').click();
        await page.waitForTimeout(2000);

        // Read local host browser clipboard
        console.log('Querying local browser clipboard...');
        const localClipboard = await page.evaluate(() => navigator.clipboard.readText());

        console.log(`Expected : "${copySecret}"`);
        console.log(`Got      : "${localClipboard}"`);
        expect(localClipboard).toBe(copySecret);
    });
});
