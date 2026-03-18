import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8080;
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);
const SERVER_URL = `http://localhost:${PORT}/viewer.html`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Bandwidth Configuration', () => {
    test.beforeAll(async () => {
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        
        serverProcess = spawn('docker', [
            'run', '--rm',
            '-p', `${PORT}:${PORT}/tcp`,
            '-p', `${PORT}:${PORT}/udp`,
            '-e', `PORT=${PORT}`,
            '-e', `FPS=15`,
            '-e', `DISPLAY_NUM=${DISPLAY_NUM}`,
            '-e', `TEST_MINIMAL_X11=1`,
            '-e', `WEBRTC_PUBLIC_IP=127.0.0.1`,
            'danchitnis/llrdc',
            './llrdc',
            '--port', String(PORT),
            '--display-num', String(DISPLAY_NUM),
            '--fps', '15',
            '--webrtc-public-ip', '127.0.0.1'
        ], {
            stdio: 'pipe',
            detached: false,
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
            }, 30000);

            const onData = (data: Buffer) => {
                const output = data.toString();
                outputBuffer += output;
                if (output.includes(`Server listening on`)) {
                    clearTimeout(timeout);
                    setTimeout(resolve, 5000);
                }
            };

            serverProcess.stdout?.on('data', onData);
            serverProcess.stderr?.on('data', onData);

            serverProcess.on('exit', (code) => {
                clearTimeout(timeout);
                if (code !== 0 && code !== null) {
                    reject(new Error(`Server exited early with code ${code}. Output:\n${outputBuffer}`));
                }
            });
        });
        console.log('Server started.');
    });

    test.afterAll(async () => {
        console.log('Stopping server...');
        if (serverProcess) {
            serverProcess.kill('SIGKILL');
        }
    });

    test('should adjust bandwidth and restart video stream', async ({ page }) => {
        test.setTimeout(60000);

        page.on('console', (msg) => console.log(`[Browser]: ${msg.text()}`));

        // Inject custom stats helper
        await page.addInitScript(() => {
            (window as any).myTestStats = () => {
                const webrtc = (window as any).webrtcManager;
                const webcodecs = (window as any).webcodecsManager;
                const webrtcTotal = (webrtc && typeof webrtc.lastTotalDecoded === 'number' && webrtc.lastTotalDecoded >= 0) ? webrtc.lastTotalDecoded : 0;
                const webcodecsTotal = (webcodecs && typeof webcodecs.totalDecoded === 'number' && webcodecs.totalDecoded >= 0) ? webcodecs.totalDecoded : 0;
                const isWebRtc = webrtc && webrtc.isWebRtcActive;
                return {
                    totalDecoded: isWebRtc ? webrtcTotal : webcodecsTotal
                };
            };
        });

        await test.step('Navigate to viewer and verify initial playback', async () => {
            await page.goto(SERVER_URL);
            
            // Verify that decoding is happening initally
            await expect.poll(async () => {
                return await page.evaluate(() => {
                    if (typeof (window as any).myTestStats !== 'function') return 0;
                    return (window as any).myTestStats().totalDecoded;
                });
            }, {
                message: 'Video should be decoding initial frames',
                timeout: 30000,
            }).toBeGreaterThan(0);
        });

        await test.step('Switch bandwidth to 1 Mbps', async () => {
            const framesBeforeConfig = await page.evaluate(() => (window as any).myTestStats().totalDecoded);

            // Select 1 Mbps from the dropdown
            await page.locator('#config-btn').click();
            await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();

            const selectLocator = page.locator('#bandwidth-select');
            await selectLocator.waitFor({ state: 'visible', timeout: 10000 });
            await selectLocator.selectOption('1');

            // Wait for decoding to resume
            await expect.poll(async () => {
                return await page.evaluate(() => (window as any).myTestStats().totalDecoded);
            }, {
                message: 'Video should have resumed decoding frames after 1 Mbps switch',
                timeout: 20000,
            }).toBeGreaterThan(framesBeforeConfig + 2);
        });

        await test.step('Switch bandwidth to 10 Mbps', async () => {
            const framesBeforeConfig2 = await page.evaluate(() => (window as any).myTestStats().totalDecoded);

            const selectLocator = page.locator('#bandwidth-select');
            await selectLocator.selectOption('10');

            await expect.poll(async () => {
                return await page.evaluate(() => (window as any).myTestStats().totalDecoded);
            }, {
                message: 'Video should have resumed decoding frames after 10 Mbps switch',
                timeout: 20000,
            }).toBeGreaterThan(framesBeforeConfig2 + 2);
        });

        // Assert Server Output reflects the bandwidth change config 
        expect(outputBuffer).toContain('Target bandwidth changed to 1 Mbps');
        expect(outputBuffer).toContain('Target bandwidth changed to 10 Mbps');
    });
});
