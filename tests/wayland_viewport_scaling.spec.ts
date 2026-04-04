import { test, expect } from '@playwright/test';
import { execSync, spawn } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-wayland-scaling-test';
const PORT = '8096';
const TARGET_VIEWPORT = { width: 1280, height: 800 };
const STREAM_SIZE = { width: 1920, height: 1080 };
const TOLERANCE_PX = 1;

type RectMetrics = {
    x: number;
    y: number;
    width: number;
    height: number;
    right: number;
    bottom: number;
};

type LayoutMetrics = {
    viewport: { width: number; height: number };
    topBar: RectMetrics;
    displayContainer: RectMetrics;
    display: RectMetrics;
    video: RectMetrics;
    intrinsicCanvas: { width: number; height: number };
    intrinsicVideo: { width: number; height: number };
    renderedContent: RectMetrics;
};

function expectRectWithin(outer: RectMetrics, inner: RectMetrics, tolerance = TOLERANCE_PX) {
    expect(inner.x).toBeGreaterThanOrEqual(outer.x - tolerance);
    expect(inner.y).toBeGreaterThanOrEqual(outer.y - tolerance);
    expect(inner.right).toBeLessThanOrEqual(outer.right + tolerance);
    expect(inner.bottom).toBeLessThanOrEqual(outer.bottom + tolerance);
}

async function collectLayoutMetrics(page: import('@playwright/test').Page): Promise<LayoutMetrics> {
    return await page.evaluate(() => {
        const toRect = (el: Element): RectMetrics => {
            const rect = el.getBoundingClientRect();
            return {
                x: rect.x,
                y: rect.y,
                width: rect.width,
                height: rect.height,
                right: rect.right,
                bottom: rect.bottom,
            };
        };

        const topBar = document.getElementById('top-bar');
        const displayContainer = document.getElementById('display-container');
        const display = document.getElementById('display') as HTMLCanvasElement | null;
        const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;

        if (!topBar || !displayContainer || !display || !video) {
            throw new Error('Required viewer elements are missing');
        }

        const displayRect = display.getBoundingClientRect();
        const videoRatio = display.width > 0 && display.height > 0 ? display.width / display.height : 0;
        const containerRatio = displayRect.height > 0 ? displayRect.width / displayRect.height : 0;

        let renderedWidth = displayRect.width;
        let renderedHeight = displayRect.height;
        let renderedX = displayRect.x;
        let renderedY = displayRect.y;

        if (videoRatio > 0) {
            if (containerRatio > videoRatio) {
                renderedWidth = displayRect.height * videoRatio;
                renderedX = displayRect.x + (displayRect.width - renderedWidth) / 2;
            } else {
                renderedHeight = displayRect.width / videoRatio;
                renderedY = displayRect.y + (displayRect.height - renderedHeight) / 2;
            }
        }

        return {
            viewport: {
                width: window.innerWidth,
                height: window.innerHeight,
            },
            topBar: toRect(topBar),
            displayContainer: toRect(displayContainer),
            display: toRect(display),
            video: toRect(video),
            intrinsicCanvas: {
                width: display.width,
                height: display.height,
            },
            intrinsicVideo: {
                width: video.videoWidth,
                height: video.videoHeight,
            },
            renderedContent: {
                x: renderedX,
                y: renderedY,
                width: renderedWidth,
                height: renderedHeight,
                right: renderedX + renderedWidth,
                bottom: renderedY + renderedHeight,
            },
        };
    });
}

test.describe('Wayland Video Scaling', () => {
    test.beforeAll(async () => {
        try { execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`); } catch { /* ignore */ }

        console.log('Starting container...');
        const containerImage = process.env.CONTAINER_IMAGE || 'danchitnis/llrdc:latest';
        execSync(`IMAGE_NAME=${containerImage.split(':')[0]} IMAGE_TAG=${containerImage.split(':')[1] || 'latest'} PORT=${PORT} VBR=false ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net --res 1080p`, { stdio: 'inherit' });

        spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        console.log('Cleaning up container...');
        try { execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`); } catch { /* ignore */ }
    });

    test('should scale down remote video to fit browser viewport without cropping', async ({ page }) => {
        await page.setViewportSize(TARGET_VIEWPORT);
        await page.goto(`http://localhost:${PORT}`);

        await expect(page.locator('#status')).toHaveText(/\[WebRTC|\[WebCodecs/i, { timeout: 20000 });
        await expect.poll(async () => {
            return await page.evaluate(() => {
                const canvas = document.getElementById('display') as HTMLCanvasElement | null;
                const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;
                return {
                    canvasWidth: canvas?.width ?? 0,
                    canvasHeight: canvas?.height ?? 0,
                    videoWidth: video?.videoWidth ?? 0,
                    videoHeight: video?.videoHeight ?? 0,
                };
            });
        }, { timeout: 30000 }).toMatchObject({
            canvasWidth: STREAM_SIZE.width,
            canvasHeight: STREAM_SIZE.height,
            videoWidth: STREAM_SIZE.width,
            videoHeight: STREAM_SIZE.height,
        });

        await expect.poll(async () => {
            return await page.evaluate(() => window.getStats().totalDecoded);
        }, { timeout: 10000 }).toBeGreaterThan(5);

        const final = await collectLayoutMetrics(page);

        console.log('FINAL VIEWPORT LAYOUT:', JSON.stringify(final, null, 2));
        await page.screenshot({ path: 'viewport-scaling-perfect.png' });

        expect(final.viewport).toEqual(TARGET_VIEWPORT);
        expect(final.intrinsicCanvas).toEqual(STREAM_SIZE);
        expect(final.intrinsicVideo).toEqual(STREAM_SIZE);

        expect(final.topBar.height).toBeGreaterThan(0);
        expect(final.displayContainer.height).toBeGreaterThan(0);

        expect(final.display.x).toBeCloseTo(final.displayContainer.x, 0);
        expect(final.display.y).toBeCloseTo(final.displayContainer.y, 0);
        expect(final.display.width).toBeCloseTo(final.displayContainer.width, 0);
        expect(final.display.height).toBeCloseTo(final.displayContainer.height, 0);

        expectRectWithin({
            x: 0,
            y: 0,
            width: final.viewport.width,
            height: final.viewport.height,
            right: final.viewport.width,
            bottom: final.viewport.height,
        }, final.topBar);
        expectRectWithin({
            x: 0,
            y: 0,
            width: final.viewport.width,
            height: final.viewport.height,
            right: final.viewport.width,
            bottom: final.viewport.height,
        }, final.displayContainer);
        expectRectWithin(final.displayContainer, final.renderedContent);
        expectRectWithin({
            x: 0,
            y: 0,
            width: final.viewport.width,
            height: final.viewport.height,
            right: final.viewport.width,
            bottom: final.viewport.height,
        }, final.renderedContent);

        expect(final.renderedContent.width / final.renderedContent.height).toBeCloseTo(STREAM_SIZE.width / STREAM_SIZE.height, 2);
        expect(final.renderedContent.width).toBeCloseTo(TARGET_VIEWPORT.width, 0);
        expect(final.renderedContent.height).toBeCloseTo((TARGET_VIEWPORT.width * STREAM_SIZE.height) / STREAM_SIZE.width, 0);
        expect(final.renderedContent.bottom).toBeLessThanOrEqual(final.viewport.height + TOLERANCE_PX);
        expect(final.renderedContent.right).toBeLessThanOrEqual(final.viewport.width + TOLERANCE_PX);

        const topPadding = final.renderedContent.y - final.displayContainer.y;
        const bottomPadding = final.displayContainer.bottom - final.renderedContent.bottom;
        expect(topPadding).toBeGreaterThanOrEqual(-TOLERANCE_PX);
        expect(bottomPadding).toBeGreaterThanOrEqual(-TOLERANCE_PX);
        expect(topPadding).toBeCloseTo(bottomPadding, 0);
    });
});
