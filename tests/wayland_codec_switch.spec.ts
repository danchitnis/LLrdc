import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-codec-switch-test';
const PORT = '8087';

test.describe('Wayland Codec Switching Verification', () => {
    test.beforeAll(async () => {
        test.setTimeout(90000);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}

        console.log('Starting container for Wayland Codec Switch test...');
        execSync(`PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
        
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}
    });

    test('should maintain stable WebRTC connection when switching from VP8 to H264', async ({ page }) => {
        await page.setViewportSize({ width: 1280, height: 819 });
        await page.goto(`http://localhost:${PORT}`);

        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });
        
        console.log('Initial Status:', await statusEl.textContent());

        // Open config menu
        await page.click('#config-btn');
        await expect(page.locator('#config-dropdown')).not.toHaveClass(/hidden/);

        // Switch to H.264 (CPU)
        console.log('Switching to H.264...');
        await page.selectOption('#video-codec-select', 'h264');

        // Monitor for oscillation or failure
        console.log('Monitoring status for 10 seconds...');
        const statuses: string[] = [];
        const startTime = Date.now();
        while (Date.now() - startTime < 10000) {
            const currentStatus = await statusEl.textContent() || '';
            statuses.push(currentStatus);
            
            // If it ever shows WebCodecs after initially being WebRTC, it might be the "oscillation"
            if (currentStatus.includes('WebCodecs')) {
                console.log('Detected WebCodecs fallback during/after switch!');
            }
            
            await page.waitForTimeout(200);
        }

        const finalStatus = await statusEl.textContent() || '';
        console.log('Final Status:', finalStatus);

        // Analysis of captured statuses
        const hadWebCodecs = statuses.some(s => s.includes('WebCodecs'));
        console.log('Had WebCodecs during transition:', hadWebCodecs);

        // If it switched to WebCodecs, it means WebRTC was interrupted
        // The user says "oscillating", so it probably goes WebRTC -> WebCodecs -> WebRTC
        
        expect(hadWebCodecs, 'WebRTC should NOT fall back to WebCodecs during codec switch').toBe(false);
        expect(finalStatus).toContain('WebRTC');
    });
});
