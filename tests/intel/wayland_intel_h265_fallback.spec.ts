import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady, waitForStreamingFrames } from '../helpers';

const PORT = 8925 + Math.floor(Math.random() * 100);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-intel-h265-fallback-${PORT}`;

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

test.describe('Wayland Intel H.265 Fallback', () => {
    test.beforeAll(async () => {
        test.setTimeout(90000);
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}

        execSync(`./docker-run.sh --detach --name ${CONTAINER_NAME} --intel --host-net`, {
            env: {
                ...process.env,
                IMAGE_TAG: 'latest',
                PORT: PORT.toString(),
                HOST_PORT: PORT.toString(),
                CONTAINER_NAME,
                VIDEO_CODEC: 'h265_qsv',
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

    test('should fall back from Intel h265_qsv to CPU h265 in compat mode', async ({ page }) => {
        test.setTimeout(120000);

        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, {
            timeout: 30000,
            message: 'Wait for Intel compat server readiness',
        }).toMatchObject({
            ready: true,
            acceleratorMode: 'intel',
            directBuffer: {
                captureMode: 'compat',
                active: false,
            },
            useIntel: true,
        });

        const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
        expect(logs).toContain('Intel H.265 hardware encode is not supported on this FFmpeg/driver stack; falling back to CPU h265');

        await page.goto(SERVER_URL);
        await page.click('body');

        await expect(page.locator('#status')).toContainText(/\[h265\]/i, { timeout: 45000 });
        await expect(page.locator('#video-codec-select')).toHaveValue('h265');
        await expect(page.locator('#video-codec-select option[value="h265_qsv"]')).toHaveCount(0);

        await waitForStreamingFrames(page, 'Wait for sustained Intel compat fallback to CPU H.265');
    });
});
