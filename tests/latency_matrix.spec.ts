import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import net from 'net';
import { fileURLToPath } from 'url';
import fs from 'fs';

// --- TEST CONFIGURATION MATRIX ---
const RESOLUTIONS = [
    { label: '720p', w: 1280, h: 720 },
    { label: '1080p', w: 1920, h: 1080 },
    { label: '4k', w: 3840, h: 2160 }
];

const BANDWIDTHS = ['1', '5', '20'];
const CPU_EFFORTS = ['8']; // Fixed to 8 for fastest performance
const CPU_THREADS = ['4'];      // Fixed parameter
const FRAMERATES = ['30', '60', '120'];
const SAMPLES_PER_CONFIG = 5;
// ---------------------------------

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

let serverProcess: ChildProcess;
let serverPort: number;
let serverUrl: string;

async function getFreePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();
        server.unref();
        server.on('error', reject);
        server.listen(0, () => {
            const port = (server.address() as net.AddressInfo).port;
            server.close(() => resolve(port));
        });
    });
}

test.beforeAll(async () => {
    serverPort = await getFreePort();
    serverUrl = `http://localhost:${serverPort}`;
    console.log(`Starting server on port ${serverPort}...`);

    const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

    serverProcess = spawn('npm', ['start'], {
        env: { ...process.env, PORT: String(serverPort), FPS: '30', DISPLAY_NUM: DISPLAY_NUM.toString() },
        stdio: 'pipe',
        detached: false
    });

    serverProcess.stdout?.on('data', (data) => console.log(`[Server]: ${data}`));
    serverProcess.stderr?.on('data', (data) => console.error(`[Server Error]: ${data}`));

    try {
        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => reject(new Error('Timeout waiting for server start')), 20000);
            const dataHandler = (data: any) => {
                if (data.toString().includes('Server listening on')) {
                    clearTimeout(timeout);
                    resolve();
                }
            };
            serverProcess.stdout?.on('data', dataHandler);
            serverProcess.stderr?.on('data', dataHandler);
            serverProcess.on('exit', (code) => {
                if (code !== null && code !== 0) reject(new Error('Server failed to start'));
            });
        });
        console.log(`Server is ready on port ${serverPort}`);
    } catch (e) {
        console.error('Server failed to start');
        if (serverProcess) serverProcess.kill();
        throw e;
    }
});

test.afterAll(async () => {
    if (serverProcess) {
        console.log('Stopping server...');
        serverProcess.kill('SIGTERM');
        await new Promise(r => setTimeout(r, 1000));
        if (!serverProcess.killed) serverProcess.kill('SIGKILL');
    }
});

test('matrix latency test', async ({ page }) => {
    test.setTimeout(600000);
    await page.goto(serverUrl);

    // Wait for stream to start decoding
    await page.waitForFunction(() => {
        const stats = (window as any).getStats?.();
        return stats && stats.fps > 0;
    }, null, { timeout: 15000 });

    // Inject fast downscaled pixel change detection function
    await page.evaluate(() => {
        (window as any).monitorPixelChange = (timeout: number = 5000) => {
            return new Promise((resolve, reject) => {
                const display = document.getElementById('display') as HTMLVideoElement | HTMLCanvasElement;
                if (!display) return reject('No display element');

                const canvas = document.createElement('canvas');
                canvas.width = 200;
                canvas.height = 200;
                const ctx = canvas.getContext('2d', { willReadFrequently: true });
                if (!ctx) return reject('No context');

                ctx.drawImage(display, 0, 0, 200, 200);
                const baseline = ctx.getImageData(0, 0, 200, 200).data;
                const start = performance.now();

                function check() {
                    if (performance.now() - start > timeout) return resolve({ time: -1 });

                    ctx!.drawImage(display, 0, 0, 200, 200);
                    const current = ctx!.getImageData(0, 0, 200, 200).data;

                    let changedPixels = 0;
                    for (let i = 0; i < baseline.length; i += 4) {
                        const d = Math.abs(current[i] - baseline[i]) +
                            Math.abs(current[i + 1] - baseline[i + 1]) +
                            Math.abs(current[i + 2] - baseline[i + 2]);
                        if (d > 30) changedPixels++;
                    }

                    if (changedPixels > 5) {
                        resolve({ time: performance.now() });
                    } else {
                        requestAnimationFrame(check);
                    }
                }
                check();
            });
        };
    });

    // Spawn xeyes to track mouse movement
    await page.evaluate(() => {
        const ws = new WebSocket(window.location.href.replace('http', 'ws'));
        ws.onopen = () => ws.send(JSON.stringify({ type: 'spawn', command: 'xeyes' }));
    });
    await page.waitForTimeout(2000);

    const results: any[] = [];
    let lowestMedian = Infinity;
    let bestConfig = null;

    // Output file moved to test-results folder
    const reportDir = path.join(__dirname, '../test-results');
    if (!fs.existsSync(reportDir)) fs.mkdirSync(reportDir, { recursive: true });
    const reportPath = path.join(reportDir, 'latency_matrix_results.txt');

    fs.writeFileSync(reportPath, 'Latency Matrix Results (CBR Only)\n======================\n\n');

    const overlay = page.locator('#input-overlay');

    for (const res of RESOLUTIONS) {
        console.log(`Setting resolution to ${res.label} (${res.w}x${res.h})`);
        await page.setViewportSize({ width: res.w, height: res.h });

        await expect.poll(async () => {
            return await page.evaluate(() => {
                const canvas = document.getElementById('display') as HTMLCanvasElement;
                return canvas.width;
            });
        }, { timeout: 15000 }).toEqual(res.w);

        await page.waitForTimeout(2000);

        for (const bw of BANDWIDTHS) {
            for (const effort of CPU_EFFORTS) {
                for (const threads of CPU_THREADS) {
                    for (const framerate of FRAMERATES) {
                        console.log(`Testing: ${res.label}, BW: ${bw}Mbps, Effort: ${effort}, Threads: ${threads}, FPS: ${framerate}`);

                        await page.evaluate(({ bw, effort, threads, framerate }) => {
                            const bwSelect = document.getElementById('bandwidth-select') as HTMLSelectElement;
                            bwSelect.value = bw;
                            bwSelect.dispatchEvent(new Event('change'));

                            const vbrCheck = document.getElementById('vbr-checkbox') as HTMLInputElement;
                            if (vbrCheck.checked) {
                                vbrCheck.checked = false;
                                vbrCheck.dispatchEvent(new Event('change'));
                            }

                            const effSlider = document.getElementById('cpu-effort-slider') as HTMLInputElement;
                            effSlider.value = effort;
                            effSlider.dispatchEvent(new Event('input'));
                            effSlider.dispatchEvent(new Event('change'));

                            const thrSelect = document.getElementById('cpu-threads-select') as HTMLSelectElement;
                            thrSelect.value = threads;
                            thrSelect.dispatchEvent(new Event('change'));

                            const fpsSelect = document.getElementById('framerate-select') as HTMLSelectElement;
                            if (fpsSelect) {
                                fpsSelect.value = framerate;
                                fpsSelect.dispatchEvent(new Event('change'));
                            }
                        }, { bw, effort, threads, framerate });

                        await page.waitForTimeout(2000);

                        const latencies: number[] = [];

                        for (let i = 0; i < SAMPLES_PER_CONFIG; i++) {
                            const startX = i % 2 === 0 ? 100 : res.w - 100;
                            const startY = i % 2 === 0 ? 100 : res.h - 100;
                            const targetX = i % 2 === 0 ? res.w - 100 : 100;
                            const targetY = i % 2 === 0 ? res.h - 100 : 100;

                            await overlay.hover({ position: { x: startX, y: startY } });
                            await page.waitForTimeout(500);

                            const detectionPromise = page.evaluate(() => {
                                return (window as any).monitorPixelChange();
                            });

                            const moveStart = await page.evaluate(() => performance.now());
                            await overlay.hover({ position: { x: targetX, y: targetY } });

                            const result: any = await detectionPromise;
                            if (result.time !== -1) {
                                const latency = (result.time as number) - (moveStart as number);
                                latencies.push(latency);
                            } else {
                                console.log('  Timeout on sample');
                            }
                        }

                                                if (latencies.length === 0) continue;
                        
                                                const measuredFps = await page.evaluate(() => {
                                                    return (window as any).getStats?.().fps || 0;
                                                });
                        
                                                latencies.sort((a, b) => a - b);
                                                const mid = Math.floor(latencies.length / 2);
                                                const median = latencies.length % 2 !== 0 ? latencies[mid] : (latencies[mid - 1] + latencies[mid]) / 2;
                        
                                                console.log(`  Median Latency: ${median.toFixed(2)}ms, Measured FPS: ${measuredFps.toFixed(1)}`);
                                                
                                                const configStr = `${res.label} | BW: ${bw}Mbps | Effort: ${effort} | Threads: ${threads} | Target FPS: ${framerate} | Measured FPS: ${measuredFps.toFixed(1)}`;
                                                fs.appendFileSync(reportPath, `${configStr} -> Median: ${median.toFixed(2)}ms (Samples: ${latencies.map(l => l.toFixed(1)).join(', ')})\n`);
                        if (median < lowestMedian) {
                            lowestMedian = median;
                            bestConfig = configStr;
                        }
                    }
                }
            }
        }
    }

    const summary = `\nLowest Latency Combination:\n${bestConfig} -> ${lowestMedian.toFixed(2)}ms\n`;
    fs.appendFileSync(reportPath, summary);
    console.log(summary);
    console.log(`Full report written to: ${reportPath}`);

    expect(lowestMedian).toBeLessThan(1000);
});
