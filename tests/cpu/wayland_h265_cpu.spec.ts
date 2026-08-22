import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, readClientStats, waitForServerReady } from '../helpers';

const PORT = 8900 + Math.floor(Math.random() * 200);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-h265-cpu-${PORT}`;

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

test.describe('Wayland CPU H.265', () => {
    test.beforeAll(async () => {
        test.setTimeout(90000);
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}

        execSync(`./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net`, {
            env: {
                ...process.env,
                IMAGE_TAG: 'latest',
                PORT: PORT.toString(),
                HOST_PORT: PORT.toString(),
                CONTAINER_NAME,
                VIDEO_CODEC: 'h265',
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

    test('should stream H.265 on CPU in compat mode', async ({ page }) => {
        test.setTimeout(120000);

        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, {
            timeout: 30000,
            message: 'Wait for CPU H.265 server readiness in compat mode',
        }).toMatchObject({
            ready: true,
            acceleratorMode: 'cpu',
            directBuffer: {
                captureMode: 'compat',
                active: false,
            },
            useIntel: false,
        });

        await page.goto(SERVER_URL);
        const hevcSupported = await page.evaluate(async () => {
            if (typeof VideoDecoder === 'undefined') return false;
            try {
                const support = await VideoDecoder.isConfigSupported({
                    codec: 'hev1.1.6.L120.90',
                    optimizeForLatency: true,
                    hardwareAcceleration: 'prefer-software',
                });
                return support.supported;
            } catch {
                return false;
            }
        });
        test.skip(!hevcSupported, 'Installed Google Chrome does not expose an HEVC WebCodecs decoder');

        await page.click('body');

        await expect(page.locator('#status')).toContainText(/\[(WebTransport|WebSocket|WebCodecs) h265\]/i, { timeout: 45000 });
        await expect.poll(async () => {
            const stats = await readClientStats(page);
            return stats.totalDecoded > 10 && stats.fps > 0;
        }, {
            timeout: 30000,
            message: 'Wait for active CPU H.265 decoding',
        }).toBe(true);
    });
});
