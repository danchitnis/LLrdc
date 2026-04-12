import { test, expect } from '@playwright/test';
import { execSync, spawn } from 'child_process';
import { getContainerImage, waitForServerReady } from '../helpers';

const CONTAINER_NAME = 'llrdc-intel-test';
const PORT = '8082';

test.describe('Intel GPU Metrics Verification', () => {
    test.setTimeout(90000);
    test.beforeAll(async () => {
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {
            // ignore
        }

        console.log('Starting container with --intel...');
        const containerImage = getContainerImage('intel');
        // We use --intel and --debug to verify the metric
        execSync(`IMAGE_NAME=${containerImage.name} IMAGE_TAG=${containerImage.tag} PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --intel --host-net --debug`, { stdio: 'inherit' });
        
        spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
        
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {
            // ignore
        }
    });

    test('should display Intel GPU metrics in the status bar', async ({ page }) => {
        page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
        await page.goto(`http://localhost:${PORT}`);

        // Wait for WebRTC connection
        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/🚀/i, { timeout: 20000 });

        // Check if the encoder metric appears in the status bar
        // Pattern: [codec 🚀 GPU] res | 30 FPS | Lat: Xms | Ping: Xms | BW: XMb | CPU: X% | Enc: X%
        
        console.log('Observing metrics for 12 seconds...');
        for (let i = 0; i < 12; i++) {
            // Activity to trigger encoding
            await page.mouse.move(100 + Math.random() * 800, 100 + Math.random() * 600);
            
            const statusText = await statusEl.textContent() || '';
            console.log(`[${i+1}/12] Status: ${statusText}`);
            
            if (i === 0) {
                expect(statusText).toContain('Enc:');
            }
            
            await page.waitForTimeout(1000);
        }

        
        const finalStatusText = await statusEl.textContent();
        console.log('Final status bar text:', finalStatusText);
    });
});
