import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForServerReady } from './helpers';

const CONTAINER_NAME = 'llrdc-codec-switch-test';
const PORT = '8090'; 

test.use({ headless: false });

test.describe('Wayland Codec Switch (Single Container)', () => {
    
    test.beforeAll(async () => {
        test.setTimeout(120000);
        try { execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`); } catch (e) {}

        console.log('Starting container for Dynamic Switching test at 720p...');
        execSync(`PORT=${PORT} ./docker-run.sh --detach --name ${CONTAINER_NAME} --host-net --res 720p`);
        await waitForServerReady(`http://localhost:${PORT}`);
    });

    test.afterAll(async () => {
        try { execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`); } catch (e) {}
    });

    const codecs = ['h264', 'av1', 'vp8'];

    for (const codec of codecs) {
        test(`should switch to ${codec} via UI and display content`, async ({ page }) => {
            test.setTimeout(90000);
            
            // Force Playwright viewport to 1280x720 to avoid 2x2 resolution
            await page.setViewportSize({ width: 1280, height: 720 });

            page.on('console', msg => {
                const txt = msg.text();
                if (txt.includes('totalDecoded') || txt.includes('ICE') || txt.includes('Connection') || txt.includes('Brightness')) {
                    console.log(`[BROWSER]: ${txt}`);
                }
            });

            await page.goto(`http://localhost:${PORT}`);
            
            // Open config menu
            await page.click('#config-btn');
            await expect(page.locator('#config-dropdown')).toBeVisible();

            // Set Keyframe Interval to 1s via evaluate to bypass visibility checks
            await page.evaluate(() => {
                const el = document.getElementById('keyframe-interval-select') as HTMLSelectElement;
                if (el) {
                    el.value = '1';
                    el.dispatchEvent(new Event('change'));
                }
            });

            console.log(`>>> Selecting ${codec}...`);
            await page.selectOption('#video-codec-select', codec);
            
            // Wait for the stream to settle
            await page.waitForTimeout(10000);

            await expect.poll(async () => {
                const stats = await page.evaluate(() => {
                    const s = window.getStats();
                    const video = document.getElementById('webrtc-video') as HTMLVideoElement;
                    const canvas = document.getElementById('display') as HTMLCanvasElement;
                    const ctx = canvas.getContext('2d');
                    
                    let brightness = -1;
                    if (ctx && canvas.width > 0 && canvas.height > 0) {
                        const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
                        let total = 0;
                        for (let i = 0; i < imageData.length; i += 4) {
                            total += (imageData[i] + imageData[i + 1] + imageData[i + 2]) / 3;
                        }
                        brightness = total / (canvas.width * canvas.height);
                    }

                    return {
                        ...s,
                        videoWidth: video?.videoWidth,
                        videoHeight: video?.videoHeight,
                        brightness
                    };
                });

                console.log(`[${codec}] Stats: FPS=${stats.fps}, Decoded=${stats.totalDecoded}, Size=${stats.videoWidth}x${stats.videoHeight}, Brightness=${stats.brightness.toFixed(2)}`);
                
                // Success criteria: frames are decoding AND the screen is not completely black (brightness > 1)
                return stats.totalDecoded > 0 && stats.brightness > 1;
            }, { 
                timeout: 45000, 
                message: `Failed to switch to ${codec} or screen is black` 
            }).toBeTruthy();
        });
    }
});
