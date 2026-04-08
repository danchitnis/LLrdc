import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-vbr-bw-test';
const PORT = '8087';

test.describe('Wayland VBR Bandwidth Verification', () => {
    test.beforeAll(async () => {
        test.setTimeout(60000);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}

        console.log('Starting container for Wayland VBR BW test...');
        // Start with Damage Tracking OFF so we have 30 FPS, and VBR OFF (CBR)
        execSync(`PORT=${PORT} VBR=false DAMAGE_TRACKING=false BANDWIDTH=5 ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
        
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}
    });

    test('should show significant BW drop when VBR is enabled on a static screen with 30 FPS', async ({ page }) => {
        await page.setViewportSize({ width: 1280, height: 819 });
        await page.goto(`http://localhost:${PORT}`);

        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/BW:/i, { timeout: 60000 });

        // Helper to get current BW from status text
        const getBw = async () => {
            const text = await statusEl.textContent() || '';
            const match = text.match(/BW: ([\d.]+)/);
            return match ? parseFloat(match[1]) : -1;
        };

        const getFps = async () => {
            const text = await statusEl.textContent() || '';
            const match = text.match(/FPS: (\d+)/);
            return match ? parseInt(match[1], 10) : -1;
        };

        // --- STEP 1: CBR (VBR=OFF), DT=OFF ---
        console.log('Step 1: VBR=OFF (CBR), DT=OFF. Expecting high BW (~5 Mbps)...');
        await page.waitForTimeout(10000);
        
        let cbrBwSamples = [];
        for (let i = 0; i < 5; i++) {
            cbrBwSamples.push(await getBw());
            await page.waitForTimeout(1000);
        }
        let avgCbrBw = cbrBwSamples.reduce((a, b) => a + b, 0) / cbrBwSamples.length;
        let currentFps = await getFps();
        console.log(`Avg CBR BW: ${avgCbrBw.toFixed(2)} Mbps, FPS: ${currentFps}`);
        
        // --- STEP 2: VBR=ON, DT=OFF ---
        console.log('Step 2: Enabling VBR via UI...');
        await page.click('#config-btn');
        await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();
        const vbrCheckbox = page.locator('#vbr-checkbox');
        await vbrCheckbox.check();
        
        // Wait for server to restart and stream to stabilize
        await page.waitForTimeout(10000);
        await expect(statusEl).toHaveText(/BW:/i, { timeout: 30000 });

        console.log('Step 2: VBR=ON, DT=OFF. Expecting low BW (< 1 Mbps) on static screen...');
        
        let vbrBwSamples = [];
        for (let i = 0; i < 5; i++) {
            vbrBwSamples.push(await getBw());
            await page.waitForTimeout(1000);
        }
        let avgVbrBw = vbrBwSamples.reduce((a, b) => a + b, 0) / vbrBwSamples.length;
        currentFps = await getFps();
        console.log(`Avg VBR BW: ${avgVbrBw.toFixed(2)} Mbps, FPS: ${currentFps}`);

        console.log(`Bandwidth ratio (VBR/CBR): ${(avgVbrBw / avgCbrBw).toFixed(4)}`);

        // We expect a significant drop. Even 50% drop is a good sign for VBR on static content.
        expect(avgVbrBw).toBeLessThan(avgCbrBw * 0.5);
    });
});
