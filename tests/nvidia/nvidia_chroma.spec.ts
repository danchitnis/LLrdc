import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForStreamingFrames, waitForServerReady } from '../helpers';

const CONTAINER_NAME = 'llrdc-nvidia-chroma-test';
const PORT = '9090';
const SERVER_URL = `http://localhost:${PORT}/viewer.html`;

test.describe('NVIDIA 4:4:4 Chroma Codec Selection', () => {
    // High timeout for live container operations
    test.setTimeout(120000);

    test.beforeEach(async () => {
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
        
        console.log('Starting LLrdc server with NVIDIA support...');
        const imageTag = 'nvidia-test';
        execSync(`PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --nvidia --tag ${imageTag} --network-host`, { stdio: 'inherit' });
            
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterEach(async () => {
        try {
            execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
        } catch (e) {}
    });

    test('should select H.264 4:4:4 and verify encoder profile', async ({ page }) => {
        page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
        await page.goto(SERVER_URL);
        await page.click('body');

        // Step 1: Verify initial connection
        await expect(page.locator('#status')).toContainText(/\[WebRTC/i, { timeout: 30000 });
        
        // Step 2: Ensure initial 4:2:0 stream is flowing
        console.log('Waiting for initial H.264 4:2:0 stream...');
        await waitForStreamingFrames(page, 'Initial H.264 streaming', 30000);

        // Step 3: Change to 4:4:4
        console.log('Selecting H.264 4:4:4 (NVIDIA NVENC)...');
        await page.click('#config-btn');
        await expect(page.locator('#config-dropdown')).toBeVisible();
        await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();

        await page.selectOption('#video-codec-select', 'h264_nvenc-444');

        // Step 4: Verify server-side logs using 2>&1 to capture stderr (where ffmpeg logs)
        console.log('Polling server logs for rgb_mode=yuv444 and lossless tune...');
        await expect.poll(() => {
            try {
                const logs = execSync(`docker logs ${CONTAINER_NAME} 2>&1`).toString();
                const hasRgbMode = logs.includes('rgb_mode=yuv444');
                const hasTune = logs.includes('tune=lossless');
                const hasProfile = logs.includes('High 4:4:4 Predictive') || logs.includes('profile=high444p');
                return hasRgbMode && hasTune && hasProfile;
            } catch (e) {
                return false;
            }
        }, {
            timeout: 30000,
            intervals: [2000],
            message: 'Encoder logs should eventually contain rgb_mode=yuv444, tune=lossless and 4:4:4 profile'
        }).toBe(true);

        // Step 5: Verify the 4:4:4 stream actually renders frames
        console.log('Waiting for 4:4:4 stream to stabilize and render...');
        await waitForStreamingFrames(page, 'H.264 4:4:4 streaming', 40000);
    });

    test('should fallback to 4:2:0 when selecting standard H.264 NVENC', async ({ page }) => {
        page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
        await page.goto(SERVER_URL);
        await page.click('body');

        await expect(page.locator('#status')).toContainText(/\[WebRTC/i, { timeout: 30000 });
        await waitForStreamingFrames(page, 'Initial H.264 streaming', 30000);

        console.log('Selecting standard H.264 (NVIDIA NVENC)...');
        await page.click('#config-btn');
        await expect(page.locator('#config-dropdown')).toBeVisible();
        await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();

        await page.selectOption('#video-codec-select', 'h264_nvenc');

        // Verify return to yuv420
        await expect.poll(() => {
            try {
                const logs = execSync(`docker logs ${CONTAINER_NAME} 2>&1`).toString();
                return logs.includes('rgb_mode=yuv420');
            } catch (e) {
                return false;
            }
        }, {
            timeout: 30000,
            intervals: [2000],
            message: 'Encoder logs should contain rgb_mode=yuv420'
        }).toBe(true);

        await waitForStreamingFrames(page, 'H.264 4:2:0 streaming', 40000);
    });
});
