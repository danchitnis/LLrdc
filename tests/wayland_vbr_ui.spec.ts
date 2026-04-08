import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-vbr-ui-test';
const PORT = '8086';

test.describe('Wayland VBR and Damage Tracking UI Metrics Verification', () => {
    test.beforeAll(async () => {
        test.setTimeout(60000);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}

        console.log('Starting container for Wayland VBR UI test...');
        // Start with both enabled by default for the first test case
        execSync(`PORT=${PORT} VBR=true DAMAGE_TRACKING=true ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
        
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}
    });

    test('should verify that Damage Tracking (not VBR) controls frame delivery', async ({ page }) => {
        await page.setViewportSize({ width: 1280, height: 819 });
        await page.goto(`http://localhost:${PORT}`);

        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/FPS:/i, { timeout: 60000 });

        // Helper to get current FPS from status text
        const getFps = async () => {
            const text = await statusEl.textContent() || '';
            const match = text.match(/FPS: (\d+)/);
            return match ? parseInt(match[1], 10) : -1;
        };

        // --- STEP 1: DT=ON, VBR=ON (Static Screen) ---
        console.log('Step 1: DT=ON, VBR=ON. Expecting ~0 FPS...');
        await page.waitForTimeout(10000);
        let fps = await getFps();
        console.log(`FPS: ${fps}`);
        expect(fps).toBeLessThanOrEqual(2);

        // Open config and go to Quality tab
        await page.click('#config-btn');
        await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();
        const dtCheckbox = page.locator('#damage-tracking-checkbox');
        const vbrCheckbox = page.locator('#vbr-checkbox');

        // --- STEP 2: DT=OFF, VBR=ON (Static Screen) ---
        console.log('Step 2: DT=OFF, VBR=ON. Expecting ~30 FPS...');
        await dtCheckbox.uncheck();
        await page.waitForTimeout(10000); // Wait for restart and smoothing
        
        let highFpsSamples = [];
        for (let i = 0; i < 5; i++) {
            highFpsSamples.push(await getFps());
            await page.waitForTimeout(1000);
        }
        let maxHighFps = Math.max(...highFpsSamples);
        console.log(`Max FPS with DT=OFF: ${maxHighFps}`);
        expect(maxHighFps).toBeGreaterThan(15);

        // --- STEP 3: DT=OFF, VBR=OFF (Static Screen) ---
        console.log('Step 3: DT=OFF, VBR=OFF (CBR). Expecting ~30 FPS...');
        await vbrCheckbox.uncheck();
        await page.waitForTimeout(10000);
        
        let cbrFpsSamples = [];
        for (let i = 0; i < 5; i++) {
            cbrFpsSamples.push(await getFps());
            await page.waitForTimeout(1000);
        }
        let maxCbrFps = Math.max(...cbrFpsSamples);
        console.log(`Max FPS with DT=OFF, VBR=OFF: ${maxCbrFps}`);
        expect(maxCbrFps).toBeGreaterThan(15);

        // --- STEP 4: DT=ON, VBR=OFF (Static Screen) ---
        console.log('Step 4: DT=ON, VBR=OFF. Expecting ~0 FPS...');
        await dtCheckbox.check();
        await page.waitForTimeout(10000);
        
        fps = await getFps();
        console.log(`FPS: ${fps}`);
        expect(fps).toBeLessThanOrEqual(2);
        
        console.log('Test passed: Damage Tracking is the true arbiter of frame delivery.');
    });
});
