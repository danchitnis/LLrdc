import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-vbr-ui-test';
const PORT = '8086';

test.describe('Wayland VBR UI Metrics Verification', () => {
    test.beforeAll(async () => {
        test.setTimeout(60000);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}

        console.log('Starting container for Wayland VBR UI test...');
        // VBR is enabled by default in the new implementation
        execSync(`PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`);
        
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try {
            execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
        } catch (e) {}
    });

    test('should show 0 FPS and low BW when screen is static with VBR', async ({ page }) => {
        await page.setViewportSize({ width: 1280, height: 819 });
        await page.goto(`http://localhost:${PORT}`);

        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/FPS:/i, { timeout: 60000 });

        // Wait for it to settle and initially show some FPS/BW
        await page.waitForTimeout(5000);
        let statusText = await statusEl.textContent() || '';
        console.log('Initial Status:', statusText);

        // Extract initial FPS to make sure it WAS active
        const initialFpsMatch = statusText.match(/FPS: (\d+)/);
        const initialFps = initialFpsMatch ? parseInt(initialFpsMatch[1], 10) : 0;
        console.log(`Initial FPS: ${initialFps}`);
        
        // Wait 10 seconds for metrics to drop to zero
        console.log('Waiting for idle metrics...');
        await page.waitForTimeout(10000);

        statusText = await statusEl.textContent() || '';
        console.log('Idle Status:', statusText);

        // Extract FPS and BW
        const fpsMatch = statusText.match(/FPS: (\d+)/);
        const bwMatch = statusText.match(/BW: ([\d.]+)/);

        const fps = fpsMatch ? parseInt(fpsMatch[1], 10) : -1;
        const bw = bwMatch ? parseFloat(bwMatch[1]) : -1;

        console.log(`Parsed metrics - FPS: ${fps}, BW: ${bw}`);

        // Expectations:
        expect(fps).toBeGreaterThanOrEqual(0);
        expect(fps).toBeLessThanOrEqual(2);
        expect(bw).toBeLessThan(0.1);

        // Now generate some activity and check FPS
        console.log('Generating activity...');
        
        // Launch mousepad or terminal to have something visible that might blink or move
        execSync(`docker exec -u remote -d ${CONTAINER_NAME} xfce4-terminal`);
        await page.waitForTimeout(2000);

        let maxActiveFps = 0;
        const overlay = page.locator('#input-overlay');
        await overlay.hover();
        
        console.log('Moving mouse and polling FPS...');
        for (let i = 0; i < 30; i++) {
            await page.mouse.move(100 + (i % 10) * 50, 100 + (i % 10) * 50);
            await page.waitForTimeout(200);
            
            statusText = await statusEl.textContent() || '';
            const match = statusText.match(/FPS: (\d+)/);
            if (match) {
                const currentFps = parseInt(match[1], 10);
                if (currentFps > maxActiveFps) maxActiveFps = currentFps;
            }
            if (maxActiveFps > 5) break; // Found activity
        }

        console.log(`Max Observed Active FPS: ${maxActiveFps}`);
        expect(maxActiveFps).toBeGreaterThan(5);
    });

    test('should show 0 FPS and low BW when screen is static with VBR (WebCodecs)', async ({ page }) => {
        await page.addInitScript(() => {
            class DisabledRTCPeerConnection {
                public localDescription: RTCSessionDescriptionInit | null = null;
                public connectionState: RTCPeerConnectionState = 'new';
                public iceConnectionState: RTCIceConnectionState = 'new';
                public onicecandidate: ((this: RTCPeerConnection, ev: RTCPeerConnectionIceEvent) => any) | null = null;
                public onconnectionstatechange: ((this: RTCPeerConnection, ev: Event) => any) | null = null;
                public oniceconnectionstatechange: ((this: RTCPeerConnection, ev: Event) => any) | null = null;
                public ontrack: ((this: RTCPeerConnection, ev: RTCTrackEvent) => any) | null = null;

                addTransceiver() {}
                createDataChannel() {
                    return {
                        readyState: 'closed',
                        onopen: null,
                        onclose: null,
                        close() {}
                    };
                }
                createOffer() {
                    return Promise.reject(new Error('WebRTC disabled for WebCodecs test'));
                }
                setLocalDescription() {
                    return Promise.resolve();
                }
                setRemoteDescription() {
                    return Promise.resolve();
                }
                addIceCandidate() {
                    return Promise.resolve();
                }
                getStats() {
                    return Promise.resolve(new Map());
                }
                close() {}
            }

            // Force a genuine WebCodecs-only session so the websocket fallback remains active.
            (window as unknown as { RTCPeerConnection: typeof RTCPeerConnection }).RTCPeerConnection =
                DisabledRTCPeerConnection as unknown as typeof RTCPeerConnection;
        });

        await page.setViewportSize({ width: 1280, height: 819 });
        await page.goto(`http://localhost:${PORT}`);

        const statusEl = page.locator('#status');
        await expect(statusEl).toHaveText(/WebCodecs/i, { timeout: 30000 });

        // Wait for it to settle
        await page.waitForTimeout(10000);
        let statusText = await statusEl.textContent() || '';
        const fps = await page.evaluate(() => window.webcodecsManager?.fps ?? -1);

        console.log('Idle WebCodecs Status:', statusText);
        console.log(`Idle WebCodecs FPS: ${fps}`);
        expect(fps).toBeLessThanOrEqual(2);

        // Activity check for WebCodecs
        console.log('Generating activity for WebCodecs...');
        execSync(`docker exec -u remote -d ${CONTAINER_NAME} xfce4-terminal`);
        await page.waitForTimeout(2000);

        const overlay = page.locator('#input-overlay');
        await overlay.hover();
        
        let maxActiveFps = 0;
        for (let i = 0; i < 20; i++) {
            await page.mouse.move(100 + (i % 10) * 50, 100 + (i % 10) * 50);
            await page.waitForTimeout(200);

            const currentFps = await page.evaluate(() => window.webcodecsManager?.fps ?? 0);
            if (currentFps > maxActiveFps) maxActiveFps = currentFps;
            if (maxActiveFps > 5) break;
        }
        console.log(`Max Observed WebCodecs Active FPS: ${maxActiveFps}`);
        expect(maxActiveFps).toBeGreaterThan(5);
    });
});
