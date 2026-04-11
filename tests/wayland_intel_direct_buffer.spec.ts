import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from './helpers';

const PORT = 8250 + Math.floor(Math.random() * 500);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-intel-direct-${PORT}`;

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

test.describe('Wayland Intel Direct Buffer Path', () => {
    test.beforeAll(async () => {
        test.setTimeout(90000);
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}

        console.log(`Starting server with --intel --capture-mode direct on port ${PORT}...`);
        execSync(`./docker-run.sh --intel --capture-mode direct --detach --name ${CONTAINER_NAME}`, {
            env: {
                ...process.env,
                IMAGE_TAG: 'local-test',
                PORT: PORT.toString(),
                HOST_PORT: PORT.toString(),
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

    test('should activate direct-buffer mode with Intel QSV and stream frames', async ({ page }) => {
        test.setTimeout(120000);

        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, {
            timeout: 30000,
            message: 'Wait for direct-buffer mode to be reported as active in /readyz',
        }).toMatchObject({
            ready: true,
            directBuffer: {
                requested: true,
                supported: true,
                active: true,
                captureMode: 'direct',
            },
            useIntel: true,
        });

        await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
            timeout: 20000,
            message: 'Wait for direct-buffer probe success log',
        }).toContain('Direct-buffer probe passed');

        const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
        expect(logs).toContain('hardware capture is active');

        await page.goto(SERVER_URL);
        await page.click('body');

        await expect(page.locator('#direct-buffer-status')).toHaveText(/Active/, { timeout: 30000 });
        await expect(page.locator('#status')).toContainText(/\[h264 🚀 GPU\]/, { timeout: 45000 });

        await expect.poll(async () => {
            return await page.evaluate(() => (window as any).getStats ? (window as any).getStats().totalDecoded : 0);
        }, {
            timeout: 45000,
            message: 'Wait for decoded frames on the direct-buffer path',
        }).toBeGreaterThan(0);
    });
});
