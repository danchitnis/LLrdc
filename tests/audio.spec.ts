import { test, expect } from '@playwright/test';
import { spawn, ChildProcess, execSync } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 100 + Math.floor(Math.random() * 100);

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`);
    } catch (e) {
        // ignore if no process found
    }
}
const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Audio Functionality', () => {
    test.beforeAll(async () => {
        killPort(PORT);
        console.log(`Starting server on port ${PORT} display :${DISPLAY_NUM}...`);
        serverProcess = spawn('npm', ['start'], {
            env: { ...process.env, PORT: PORT.toString(), FPS: '15', DISPLAY_NUM: DISPLAY_NUM.toString() },
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        await new Promise<void>((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
            }, 25000);

            serverProcess.stdout?.on('data', (data) => {
                const output = data.toString();
                outputBuffer += output;
                if (output.includes(`Server listening on http://0.0.0.0:${PORT}`)) {
                    clearTimeout(timeout);
                    resolve();
                }
            });

            serverProcess.stderr?.on('data', (data) => {
                outputBuffer += data.toString();
            });

            serverProcess.on('exit', (code) => {
                clearTimeout(timeout);
                reject(new Error(`Server exited early with code ${code}. Output:\n${outputBuffer}`));
            });
        });
        console.log('Server started.');
    });

    test.afterAll(async () => {
        console.log('Stopping server...');
        if (serverProcess) {
            serverProcess.kill('SIGTERM');
            await new Promise<void>((resolve) => {
                const timeout = setTimeout(() => {
                    if (!serverProcess.killed) serverProcess.kill('SIGKILL');
                    resolve();
                }, 5000);
                serverProcess.on('exit', () => {
                    clearTimeout(timeout);
                    resolve();
                });
            });
        }
        killPort(PORT);
    });

    test('should receive WebRTC audio track and decode bytes', async ({ page }) => {
        test.setTimeout(60000);

        await page.goto(SERVER_URL);

        // Wait for WebRTC connection
        await page.waitForFunction(() => {
            const statusEl = document.getElementById('status');
            return statusEl && statusEl.textContent && statusEl.textContent.includes('WebRTC');
        }, { timeout: 20000 });

        const status = await page.locator('#status').textContent();
        expect(status).toContain('WebRTC');

        // Interact to unmute
        await page.click('body');

        // Find the docker container running the server
        let containerId = '';
        try {
            containerId = execSync(`docker ps -q --filter ancestor=danchitnis/llrdc:latest`).toString().trim().split('\n')[0];
        } catch (e) {
            console.error('Failed to find container:', e);
        }

        let aplayProc: ChildProcess | null = null;
        if (containerId) {
            console.log(`Found container ${containerId}. Spawning speaker-test...`);
            // Run speaker-test inside the container
            aplayProc = spawn('docker', ['exec', containerId, 'speaker-test', '-t', 'sine', '-f', '440', '-c', '1']);
        }

        // Wait for audio to be transmitted
        await page.waitForTimeout(5000);

        // Check the WebRTC stats for the audio track
        const audioStats = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return { hasAudioTrack: false, bytesReceived: 0 };

            const stats = await rtcPeer.getStats();
            let bytesReceived = 0;
            let hasAudioTrack = false;

            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'audio') {
                    hasAudioTrack = true;
                    if (report.bytesReceived !== undefined) {
                        bytesReceived = report.bytesReceived;
                    }
                }
            });

            return { hasAudioTrack, bytesReceived };
        });

        console.log('WebRTC Audio Stats:', audioStats);

        expect(audioStats.hasAudioTrack).toBe(true);
        expect(audioStats.bytesReceived).toBeGreaterThan(0);

        // Open config menu
        await page.click('#config-btn');
        // Click Audio tab
        await page.click('[data-tab="tab-audio"]');

        // Test disabling audio
        console.log('Disabling audio...');
        await page.uncheck('#enable-audio-checkbox');
        
        // Wait for server to restart audio stream (it will just stop)
        await page.waitForTimeout(4000);
        
        const statsAfterDisable1 = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return 0;
            const stats = await rtcPeer.getStats();
            let bytes = 0;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'audio') {
                    if (report.bytesReceived !== undefined) bytes = report.bytesReceived;
                }
            });
            return bytes;
        });

        await page.waitForTimeout(2000);

        const statsAfterDisable2 = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return 0;
            const stats = await rtcPeer.getStats();
            let bytes = 0;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'audio') {
                    if (report.bytesReceived !== undefined) bytes = report.bytesReceived;
                }
            });
            return bytes;
        });
        
        console.log(`Bytes after disable 1: ${statsAfterDisable1}, 2: ${statsAfterDisable2}`);
        // Bytes should not increase significantly after being disabled
        // We allow a small increase just in case there were queued packets
        expect(statsAfterDisable2 - statsAfterDisable1).toBeLessThan(5000);

        // Test enabling audio again
        console.log('Re-enabling audio...');
        await page.check('#enable-audio-checkbox');
        await page.waitForTimeout(5000);

        const statsAfterEnable = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return 0;
            const stats = await rtcPeer.getStats();
            let bytes = 0;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'audio') {
                    if (report.bytesReceived !== undefined) bytes = report.bytesReceived;
                }
            });
            return bytes;
        });

        console.log(`Bytes after re-enable: ${statsAfterEnable}`);
        expect(statsAfterEnable).toBeGreaterThan(statsAfterDisable2);

        // Test changing audio bitrate
        console.log('Changing audio bitrate...');
        await page.selectOption('#audio-bitrate-select', '64k');
        
        // Server will restart ffmpeg for audio
        await page.waitForTimeout(8000);
        
        const statsAfterBitrate = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return 0;
            const stats = await rtcPeer.getStats();
            let bytes = 0;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'audio') {
                    if (report.bytesReceived !== undefined) bytes = report.bytesReceived;
                }
            });
            return bytes;
        });
        
        console.log(`Bytes after bitrate change: ${statsAfterBitrate}`);
        
        if (aplayProc) {
            aplayProc.kill();
        }

        if (statsAfterBitrate <= statsAfterEnable) {
            console.error('Server output buffer:\n', outputBuffer);
        }
        
        expect(statsAfterBitrate).toBeGreaterThan(statsAfterEnable);
    });
});
