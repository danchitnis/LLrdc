import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-codec-test';
const PORT = '8088'; 

test.use({ headless: false });

test.describe('Wayland All Codecs Verification', () => {
    
    const codecsToTest = ['vp8', 'h264', 'av1'];

    for (const codec of codecsToTest) {
        test(`should successfully stream ${codec} on CPU`, async ({ page }) => {
            test.setTimeout(120000);
            
            try {
                execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
            } catch (e) {}

            console.log(`Starting container for ${codec} test...`);
            // We use --host-net for WebRTC and set the initial codec via environment variable
            execSync(`PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`, {
                env: {
                    ...process.env,
                    VIDEO_CODEC: codec,
                    FPS: '30'
                }
            });
            
            await waitForServerReady(`http://localhost:${PORT}`);
            
            page.on('console', msg => {
                if (msg.text().includes('totalDecoded') || msg.text().includes('ICE') || msg.text().includes('Connection')) {
                    console.log(`[BROWSER ${codec}]: ${msg.text()}`);
                }
            });

            await page.goto(`http://localhost:${PORT}`);
            
            // Wait for totalDecoded to be greater than 0
            await expect.poll(async () => {
                const stats = await page.evaluate(() => window.getStats());
                if (stats.totalDecoded > 0) {
                    console.log(`[${codec}] SUCCESS: FPS=${stats.fps}, totalDecoded=${stats.totalDecoded}`);
                }
                return stats.totalDecoded;
            }, { 
                timeout: 45000,
                message: `Wait for totalDecoded > 0 for codec ${codec}`
            }).toBeGreaterThan(0);

            // Cleanup container
            try {
                execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
            } catch (e) {}
        });
    }
});
