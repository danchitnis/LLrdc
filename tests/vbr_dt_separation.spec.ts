import { test, expect } from '@playwright/test';
import { execSync, spawn } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-vbr-test';
const PORT = '8081';

test.describe('VBR and Damage Tracking Separation', () => {
    test.beforeAll(async () => {
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) { /* ignore */ }

        console.log('Starting container...');
        const containerImage = process.env.CONTAINER_IMAGE || 'danchitnis/llrdc:latest';
        execSync(`IMAGE_NAME=${containerImage.split(':')[0]} IMAGE_TAG=${containerImage.split(':')[1] || 'latest'} PORT=${PORT} VBR=false DAMAGE_TRACKING=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`, { stdio: 'inherit' });
        
        spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) { /* ignore */ }
    });

    const getMetricsFromUI = async (page: any) => {
        const text = await page.locator('#status').textContent();
        const fpsMatch = text.match(/FPS: ([\d.]+)/);
        const bwMatch = text.match(/BW: ([\d.]+)/);
        return {
            fps: fpsMatch ? parseFloat(fpsMatch[1]) : 0,
            bw: bwMatch ? parseFloat(bwMatch[1]) : 0
        };
    };

    const measureMetrics = async (page: any, durationSec: number, moving: boolean) => {
        // Kickstart stream with motion
        for (let i = 0; i < 5; i++) {
            await page.mouse.move(100 + i * 50, 100 + i * 50);
            await page.waitForTimeout(200);
        }
        
        // Wait for stats to reflect current mode (stale data from previous mode can linger)
        await page.waitForTimeout(5000);

        const fpsSamples: number[] = [];
        const bwSamples: number[] = [];
        
        let moveTimer: any;
        if (moving) {
            moveTimer = setInterval(async () => {
                await page.mouse.move(100 + Math.random() * 500, 100 + Math.random() * 500);
            }, 100);
        }

        for (let i = 0; i < durationSec; i++) {
            const metrics = await getMetricsFromUI(page);
            fpsSamples.push(metrics.fps);
            bwSamples.push(metrics.bw);
            await page.waitForTimeout(1000);
        }

        if (moveTimer) clearInterval(moveTimer);

        const avgFps = fpsSamples.reduce((a, b) => a + b, 0) / fpsSamples.length;
        const avgBw = bwSamples.reduce((a, b) => a + b, 0) / bwSamples.length;
        
        return { avgFps, avgBw };
    };

    test('should verify separation of VBR and DT', async ({ page }) => {
        test.setTimeout(240000);
        await page.goto(`http://localhost:${PORT}`);
        
        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/\[WebRTC/i, { timeout: 30000 });
        
        await page.mouse.click(10, 10);

        // 1. CBR (Default: VBR Off, DT Off)
        console.log('--- Testing CBR (VBR: Off, DT: Off) ---');
        const cbr = await measureMetrics(page, 10, false);
        console.log(`CBR: FPS=${cbr.avgFps.toFixed(1)}, BW=${cbr.avgBw.toFixed(2)} Mbps`);
        // In virtualized environment, 30 FPS might be hard, but should be > 5
        expect(cbr.avgFps).toBeGreaterThan(5); 

        // 2. VBR Only (VBR On, DT Off, Threshold 500)
        console.log('--- Testing VBR Only (VBR: On, DT: Off, Thresh: 500) ---');
        await page.click('#config-btn');
        await page.click('.config-tab-btn[data-tab="tab-quality"]');
        await page.check('#vbr-checkbox');
        await page.fill('#vbr-threshold-slider', '500');
        await page.dispatchEvent('#vbr-threshold-slider', 'change');
        await page.waitForTimeout(5000); 
        
        const vbr = await measureMetrics(page, 10, false);
        console.log(`VBR Only: FPS=${vbr.avgFps.toFixed(1)}, BW=${vbr.avgBw.toFixed(2)} Mbps`);
        expect(vbr.avgBw).toBeLessThan(cbr.avgBw);

        // 3. DT Only (VBR Off, DT On)
        console.log('--- Testing DT Only (VBR: Off, DT: On) ---');
        await page.uncheck('#vbr-checkbox');
        await page.check('#damage-tracking-checkbox');
        await page.waitForTimeout(5000);
        
        const dt = await measureMetrics(page, 10, false);
        console.log(`DT Only: FPS=${dt.avgFps.toFixed(1)}, BW=${dt.avgBw.toFixed(2)} Mbps`);
        expect(dt.avgFps).toBeLessThan(cbr.avgFps);

        // 4. VBR + DT (Both On)
        console.log('--- Testing VBR + DT (VBR: On, DT: On) ---');
        await page.check('#vbr-checkbox');
        await page.waitForTimeout(5000);
        
        const both = await measureMetrics(page, 10, false);
        console.log(`Both On: FPS=${both.avgFps.toFixed(1)}, BW=${both.avgBw.toFixed(2)} Mbps`);
        expect(both.avgFps).toBeLessThan(5);

        // 5. Moving Screen
        console.log('--- Testing Moving Screen (VBR: On, DT: On) ---');
        const moving = await measureMetrics(page, 10, true);
        console.log(`Moving: FPS=${moving.avgFps.toFixed(1)}, BW=${moving.avgBw.toFixed(2)} Mbps`);
        expect(moving.avgFps).toBeGreaterThan(5);
        
        console.log('--- Verification Complete ---');
    });
});
