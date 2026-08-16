import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, getContainerImage, waitForServerReady, waitForStreamingFrames } from '../helpers';

const PORT = 8260 + Math.floor(Math.random() * 500);
const SERVER_URL = `http://localhost:${PORT}`;
const CONTAINER_NAME = `llrdc-wayland-intel-codec-${PORT}`;
const INTEL_RENDER_NODE = process.env.INTEL_RENDER_NODE || 'D130';
const EXPECTED_RENDER_NODE = INTEL_RENDER_NODE.startsWith('/dev/dri/render') ? INTEL_RENDER_NODE : `/dev/dri/render${INTEL_RENDER_NODE}`;

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

test.describe('Wayland Intel Direct Codec Switch', () => {
    test.beforeAll(async () => {
        test.setTimeout(90000);
        const containerImage = getContainerImage('intel');
        killPort(PORT);
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}

        console.log(`Starting server with --intel --intel-device ${INTEL_RENDER_NODE} --direct-buffer --host-net on port ${PORT}...`);
        execSync(`./docker-run.sh --intel --intel-device ${INTEL_RENDER_NODE} --direct-buffer --host-net --detach --name ${CONTAINER_NAME}`, {
            env: {
                ...process.env,
                IMAGE_NAME: containerImage.name,
                IMAGE_TAG: containerImage.tag,
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

    test('should switch between Intel H.264 (4:2:0) and HEVC 4:4:4 via unified select', async ({ page }) => {
        test.setTimeout(180000);

        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, {
            timeout: 30000,
            message: 'Wait for direct-buffer mode to be reported as active in /readyz',
        }).toMatchObject({
            ready: true,
            directBuffer: {
                active: true,
            },
        });

        await page.goto(SERVER_URL);
        await page.click('body');

        // Initial default should be h264_qsv (H.264 GPU)
        await expect(page.locator('#direct-buffer-status')).toHaveText(/Active/, { timeout: 30000 });
        await expect(page.locator('#status')).toContainText(/\[h264 🚀 GPU\]/, { timeout: 45000 });
        
        // Wait for streaming on the default h264 path
        await waitForStreamingFrames(page, 'Wait for decoded frames on default H.264 path');

        // Check that UI is populated correctly (av1_qsv removed, h265_qsv-444 added)
        await page.click('#config-btn');
        const options = await page.locator('#video-codec-select option').allTextContents();
        expect(options).toContain('H.264 (Intel)');
        expect(options).toContain('HEVC 4:4:4 (Intel)');
        expect(options).not.toContain('H.265 (Intel QSV)'); // Old label
        expect(options).not.toContain('H.265 (Intel)'); // My accidental addition earlier
        expect(options).not.toContain('AV1 (Intel QSV)'); // Old AV1 removed
        
        // Ensure current selection is mapped correctly
        await expect(page.locator('#video-codec-select')).toHaveValue('h264_qsv');

        // Switch to HEVC 4:4:4 pseudo-codec
        await page.evaluate(() => {
            const select = document.getElementById('video-codec-select') as HTMLSelectElement | null;
            if (select) {
                select.value = 'h265_qsv-444';
                select.dispatchEvent(new Event('change', { bubbles: true }));
            }
        });

        // Wait for the new track to stream
        await expect(page.locator('#status')).toContainText(/\[h265 🚀 GPU\]/, { timeout: 45000 });
        
        // Note: the installed Chrome lane does not support HEVC decoding here, so use server-side frame checks.
        // We will rely on the server-side readyz check to verify the switch was successful.

        // Check backend via readyz that chroma is 444 and codec is h265_qsv or hevc_vaapi (since it's resolved internally)
        await expect.poll(async () => {
            return await fetchReadyz(SERVER_URL);
        }, { timeout: 10000, message: 'Wait for backend to report HEVC 4:4:4' }).toMatchObject({
            chroma: '444',
            videoCodec: 'h265_qsv'
        });

        // Switch back to H.264 (which implicitly sets chroma back to 420 via UI logic)
        await page.evaluate(() => {
            const select = document.getElementById('video-codec-select') as HTMLSelectElement | null;
            if (select) {
                select.value = 'h264_qsv';
                select.dispatchEvent(new Event('change', { bubbles: true }));
            }
        });

        // Wait for streaming
        await expect(page.locator('#status')).toContainText(/\[h264 🚀 GPU\]/, { timeout: 45000 });
        await waitForStreamingFrames(page, 'Wait for decoded frames after switching back to H.264');

        // Verify backend state
        const finalReadyz = await fetchReadyz(SERVER_URL);
        expect(finalReadyz.chroma).toBe('420');
        expect(finalReadyz.videoCodec).toBe('h264_qsv');
    });
});
