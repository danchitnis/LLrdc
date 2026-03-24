import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-background-check';
const PORT = '8085';

test.describe('Wayland Background Verification', () => {
  test.beforeAll(async () => {
    test.setTimeout(120000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for background verification...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 -e USE_WAYLAND=true danchitnis/llrdc:wayland-latest`);
    
    // Give it time to boot
    await new Promise(r => setTimeout(r, 20000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should verify desktop background is NOT black', async ({ page }) => {
    test.setTimeout(100000);
    page.on('console', msg => console.log(`[Browser Console] ${msg.type()}: ${msg.text()}`));
    await page.goto(`http://localhost:${PORT}`);
    
    // Wait for the video element to be visible and stable
    const video = page.locator('video');
    await expect(video).toBeVisible({ timeout: 15000 });

    // Force 1080p via viewport
    await page.setViewportSize({ width: 1920, height: 1080 });
    
    // Give it more time for everything to settle (XFCE startup + background apply)
    await new Promise(r => setTimeout(r, 30000));
    
    // Log process list
    console.log('--- CONTAINER PROCESS LIST ---');
    try {
      console.log(execSync(`docker exec ${CONTAINER_NAME} ps aux`).toString());
    } catch (e) {}
    console.log('--- END PROCESS LIST ---');

    // Take a screenshot of the video stream
    const screenshotPath = 'test-results/background_check.png';
    await video.screenshot({ path: screenshotPath });
    console.log(`Screenshot saved to ${screenshotPath}`);

    const results = await page.evaluate(() => {
      const v = document.querySelector('video') as HTMLVideoElement;
      const canvas = document.createElement('canvas');
      canvas.width = v.videoWidth;
      canvas.height = v.videoHeight;
      const ctx = canvas.getContext('2d');
      if (!ctx) return { isBlack: true, rAvg: 0, gAvg: 0, bAvg: 0 };
      
      ctx.drawImage(v, 0, 0, canvas.width, canvas.height);
      const data = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      
      let rTotal = 0, gTotal = 0, bTotal = 0;
      const step = 50; 
      let samples = 0;
      let nonWhiteSamples = 0;
      
      let sampleColors: string[] = [];
      for (let i = 0; i < data.length; i += 4 * step) {
        if (samples % 500 === 0 && sampleColors.length < 20) {
           sampleColors.push(`[${data[i]},${data[i+1]},${data[i+2]}]`);
        }
        samples++;

        // Skip purely white pixels (icons/text) to get a better background average
        if (data[i] > 240 && data[i+1] > 240 && data[i+2] > 240) continue;

        rTotal += data[i];
        gTotal += data[i+1];
        bTotal += data[i+2];
        nonWhiteSamples++;
      }
      
      const rAvg = rTotal / nonWhiteSamples;
      const gAvg = gTotal / nonWhiteSamples;
      const bAvg = bTotal / nonWhiteSamples;
      
      console.log(`Sample pixels: ${sampleColors.join(', ')}`);
      console.log(`Background Average Color (excluding white): R=${rAvg}, G=${gAvg}, B=${bAvg}`);
      
      return {
        isBlack: (rAvg < 5 && gAvg < 5 && bAvg < 5),
        rAvg, gAvg, bAvg
      };
    });

    expect(results.isBlack, `The desktop background is black (R=${results.rAvg}, G=${results.gAvg}, B=${results.bAvg}), but it should be XFCE Blue!`).toBe(false);
  });
});
