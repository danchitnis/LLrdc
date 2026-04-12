import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from '../helpers';

const PORT = 8200 + Math.floor(Math.random() * 500);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-direct-buffer-${PORT}`;

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

    test('should activate direct-buffer mode and stream frames end to end', async ({ page }) => {
        test.setTimeout(120000);

        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, {
            timeout: 30000,
            message: 'Wait for direct-buffer mode to be reported as active in /readyz',
        }).toMatchObject({
            ready: true,
            acceleratorMode: 'nvidia',
            directBuffer: {
                requested: true,
                supported: true,
                active: true,
                captureMode: 'direct',
                screencopyAvailable: true,
                linuxDmabufAvailable: true,
            },
        });

        await expect.poll(() => execSync(`docker logs ${CONTAINER_NAME}`).toString(), {
            timeout: 20000,
            message: 'Wait for direct-buffer probe success log',
        }).toContain('Direct-buffer probe passed');

        const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
        expect(logs).not.toContain("Permission denied");
        expect(logs).not.toContain("Failed to open '/dev/dri/renderD128'");

        await page.goto(SERVER_URL);
        await page.click('body');

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
    });
});
