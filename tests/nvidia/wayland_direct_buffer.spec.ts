import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from '../helpers';

const PORT = 8200 + Math.floor(Math.random() * 500);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-direct-buffer-${PORT}`;
const ALLOW_DIRECT_BUFFER_FALLBACK = process.env.LLRDC_ALLOW_DIRECT_BUFFER_FALLBACK === '1';

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

test.describe('Wayland Direct Buffer GPU Path', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}

        execSync(`./docker-run.sh --nvidia --capture-mode direct --detach --name ${CONTAINER_NAME} --host-net`, {
            env: {
                ...process.env,
                PORT: PORT.toString(),
                HOST_PORT: PORT.toString(),
                VIDEO_CODEC: 'h264_nvenc',
                FPS: '60',
                RESOLUTION: '1920x1080',
                VBR: 'false',
                CONTAINER_NAME,
            },
            stdio: 'inherit',
        });

        await waitForServerReady(SERVER_URL, 60000);
    });

    test.afterAll(async () => {
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
    });

    test('installed Chrome validates direct H.264 1080p60, input, and reconnection', async ({ page }) => {
        test.setTimeout(90000);
        await page.goto(SERVER_URL);
        await page.click('body');

        const readyz: any = await fetchReadyz(SERVER_URL);
        expect(readyz).toMatchObject({
            acceleratorMode: 'nvidia',
            videoCodec: 'h264_nvenc',
            directBuffer: { requested: true, active: true, zeroCopyValidated: true, captureMode: 'direct' },
        });
        await expect(page.locator('#status')).toContainText(/WebTransport|WebSocket/, { timeout: 30000 });
        await expect.poll(async () => (await page.evaluate(() => window.getStats?.().totalDecoded ?? 0)), {
            timeout: 30000,
        }).toBeGreaterThan(0);

        const beforeInput = await page.evaluate(() => window.getStats?.().totalDecoded ?? 0);
        await page.mouse.move(320, 240);
        await expect.poll(async () => (await page.evaluate(() => window.getStats?.().totalDecoded ?? 0)), {
            timeout: 20000,
        }).toBeGreaterThan(beforeInput);

        await page.reload();
        await expect(page.locator('#status')).toContainText(/WebTransport|WebSocket/, { timeout: 30000 });
        await expect.poll(async () => (await page.evaluate(() => window.getStats?.().totalDecoded ?? 0)), {
            timeout: 30000,
        }).toBeGreaterThan(0);
    });

    test('should activate direct-buffer mode and stream frames end to end or fail closed gracefully', async ({ page }) => {
        test.setTimeout(60000);

        // Load the page first to trigger capture start on the server
        await page.goto(SERVER_URL);
        await page.click('body');

        // Give the server 5 seconds to run the capture or fail/exit
        await page.waitForTimeout(5000);

        let readyz: any = null;
        try {
            readyz = await fetchReadyz(SERVER_URL);
        } catch (e) {}
        expect(readyz).not.toBeNull();

        if (readyz.directBuffer && readyz.directBuffer.supported && readyz.directBuffer.zeroCopyValidated) {
            expect(readyz).toMatchObject({
                ready: true,
                acceleratorMode: 'nvidia',
                directBuffer: {
                    requested: true,
                    supported: true,
                    active: true,
                    captureMode: 'direct',
                    screencopyAvailable: true,
                    linuxDmabufAvailable: true,
                    backend: 'nvidia-native',
                    zeroCopyValidated: true,
                },
            });

            await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
                timeout: 20000,
                message: 'Wait for direct-buffer probe success log',
            }).toContain('Direct-buffer probe passed');

            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            expect(logs).not.toContain("Permission denied");
            expect(logs).not.toContain("Failed to open '/dev/dri/renderD128'");

            await expect(page.locator('#direct-buffer-status')).toHaveText(/Active/, { timeout: 30000 });
            await expect(page.locator('#status')).toContainText(/\[.*\]/, { timeout: 45000 });

            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
            }, {
                timeout: 45000,
                message: 'Wait for decoded frames on the direct-buffer path',
            }).toBeGreaterThan(0);

            await expect.poll(async () => {
                const before = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
                await page.mouse.move(160, 160);
                await page.waitForTimeout(1500);
                const after = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
                return after - before;
            }, {
                timeout: 30000,
                message: 'Verify the stream continues advancing after user activity',
            }).toBeGreaterThan(0);
        } else {
            console.log(`[Test] Direct capture mode is unsupported headlessly on this context. Reason: ${readyz.directBuffer ? readyz.directBuffer.reason : 'unknown'}`);
            if (!ALLOW_DIRECT_BUFFER_FALLBACK) {
                throw new Error(`Direct-buffer fallback is disabled by default. Reason: ${readyz.directBuffer ? readyz.directBuffer.reason : 'unknown'}`);
            }
            expect(readyz.directBuffer).toMatchObject({
                requested: true,
                active: false,
            });
            expect(readyz.directBuffer.reason).toContain('exited without producing frames');
        }
    });

    test('should support H.264 4:4:4 chroma switching under direct-buffer and stream successfully', async ({ page }) => {
        test.setTimeout(60000);

        // Load the page first to trigger capture start on the server
        await page.goto(SERVER_URL);
        await page.click('body');

        // Wait for the initial connection
        await expect(page.locator('#status')).toContainText(/\[WebTransport|\[WebSocket/i, { timeout: 30000 });

        let readyz: any = null;
        try {
            readyz = await fetchReadyz(SERVER_URL);
        } catch (e) {}
        expect(readyz).not.toBeNull();

        if (readyz.directBuffer && readyz.directBuffer.supported && readyz.directBuffer.zeroCopyValidated) {
            // Open configuration panel, switch to 4:4:4 NVENC
            await page.click('#config-btn');
            await expect(page.locator('#config-dropdown')).toBeVisible();
            await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();
            await page.selectOption('#video-codec-select', 'h264_nvenc-444');

            // Verify server transitions to 4:4:4
            await expect.poll(async () => {
                const r = await fetchReadyz(SERVER_URL);
                return r.videoCodec === 'h264_nvenc' && r.chroma === '444';
            }, {
                timeout: 15000,
                message: 'Wait for server to transition to h264_nvenc-444'
            }).toBe(true);

            // Verify the stream actively renders frames under 4:4:4
            await expect.poll(async () => {
                return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
            }, {
                timeout: 25000,
                message: 'Wait for decoded frames on the direct-buffer 4:4:4 chroma path',
            }).toBeGreaterThan(0);

            // Verify the stream continues advancing after user activity
            await expect.poll(async () => {
                const before = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
                await page.mouse.move(180, 180);
                await page.waitForTimeout(1500);
                const after = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
                return after - before;
            }, {
                timeout: 30000,
                message: 'Verify the stream continues advancing after user activity on 4:4:4',
            }).toBeGreaterThan(0);

            // Verify via readyz that directBuffer is active, and codec and chroma are correct
            const finalReadyz: any = await fetchReadyz(SERVER_URL);
            expect(finalReadyz.directBuffer.active).toBe(true);
            expect(finalReadyz.videoCodec).toBe('h264_nvenc');
            expect(finalReadyz.chroma).toBe('444');
        } else {
            console.log(`[Test] Direct capture mode is unsupported headlessly on this context. Skipping 4:4:4 chroma validation.`);
        }
    });

    test('should support H.265 (HEVC) 4:2:0 and 4:4:4 switching under direct-buffer and stream successfully via indirect checks', async ({ page }) => {
        test.setTimeout(90000);

        // Load the page first to trigger capture start on the server
        await page.goto(SERVER_URL);
        await page.click('body');

        // Wait for the initial connection
        await expect(page.locator('#status')).toContainText(/\[WebTransport|\[WebSocket/i, { timeout: 30000 });

        let readyz: any = null;
        try {
            readyz = await fetchReadyz(SERVER_URL);
        } catch (e) {}
        expect(readyz).not.toBeNull();

        if (readyz.directBuffer && readyz.directBuffer.supported && readyz.directBuffer.zeroCopyValidated) {
            // Open configuration panel, switch to H.265 (NVIDIA NVENC)
            await page.click('#config-btn');
            await expect(page.locator('#config-dropdown')).toBeVisible();
            await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();
            await page.selectOption('#video-codec-select', 'h265_nvenc');

            // Verify server transitions to h265_nvenc and directBuffer is active
            await expect.poll(async () => {
                const r = await fetchReadyz(SERVER_URL);
                return r.videoCodec === 'h265_nvenc' && r.chroma === '420' && r.directBuffer?.active === true;
            }, {
                timeout: 25000,
                message: 'Wait for server to transition to h265_nvenc 420 direct-buffer path'
            }).toBe(true);

            // Verify that the video is actively streaming by checking webtransportFps/websocketFps (received frame rate) >= 20
            await expect.poll(async () => {
                const stats = await page.evaluate(() => window.getStats ? window.getStats() : null);
                if (!stats) return 0;
                return stats.webtransportFps;
            }, {
                timeout: 25000,
                message: 'Wait for H.265 4:2:0 stream frame rate to reach ~30 FPS on client'
            }).toBeGreaterThanOrEqual(20);

            // Switch to HEVC 4:4:4
            await page.selectOption('#video-codec-select', 'h265_nvenc-444');

            // Verify server transitions to h265_nvenc-444 and directBuffer is active
            await expect.poll(async () => {
                const r = await fetchReadyz(SERVER_URL);
                return r.videoCodec === 'h265_nvenc' && r.chroma === '444' && r.directBuffer?.active === true;
            }, {
                timeout: 25000,
                message: 'Wait for server to transition to h265_nvenc 444 direct-buffer path'
            }).toBe(true);

            // Verify that the video is actively streaming in HEVC 4:4:4 with webtransportFps/websocketFps >= 20
            await expect.poll(async () => {
                const stats = await page.evaluate(() => window.getStats ? window.getStats() : null);
                if (!stats) return 0;
                return stats.webtransportFps;
            }, {
                timeout: 25000,
                message: 'Wait for H.265 4:4:4 stream frame rate to reach ~30 FPS on client'
            }).toBeGreaterThanOrEqual(20);

            // Send some mouse movements to keep stream active
            await page.mouse.move(180, 180);
            await page.waitForTimeout(1500);
            await page.mouse.move(120, 120);

            // Verify via readyz that directBuffer is active, and codec and chroma are correct
            const finalReadyz: any = await fetchReadyz(SERVER_URL);
            expect(finalReadyz.directBuffer.active).toBe(true);
            expect(finalReadyz.videoCodec).toBe('h265_nvenc');
            expect(finalReadyz.chroma).toBe('444');
        } else {
            console.log(`[Test] Direct capture mode is unsupported headlessly on this context. Skipping H.265 / HEVC direct-buffer validation.`);
        }
    });
});
