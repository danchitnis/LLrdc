import { test, expect } from '@playwright/test';
import { spawn, execSync } from 'child_process';

// Use a local tag for testing
const CONTAINER_IMAGE = process.env.CONTAINER_IMAGE || 'llrdc:wayland-local';
const CONTAINER_NAME = 'llrdc-wayland-audio-test';
const PORT = '8088';

test.describe('Wayland Audio E2E', () => {
  test.beforeAll(async () => {
    test.setTimeout(120000);
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log(`Starting container ${CONTAINER_NAME} using image ${CONTAINER_IMAGE}...`);
    try {
        execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 -e USE_WAYLAND=true -e ENABLE_AUDIO=true ${CONTAINER_IMAGE}`);
        // Log container output in background
        spawn('docker', ['logs', '-f', CONTAINER_NAME], { stdio: 'inherit' });
    } catch (e) {
        throw new Error(`Failed to start container. Make sure you have built the image with: docker build -f Dockerfile.wayland -t ${CONTAINER_IMAGE} .`);
    }
    
    // Wait for the server and components (XFCE, PulseAudio) to start
    await new Promise(r => setTimeout(r, 20000));
  });

  test.afterAll(async ({ }, testInfo) => {
    if (testInfo.status !== 'passed') {
        console.log(`Test failed, leaving container ${CONTAINER_NAME} running for inspection.`);
        return;
    }
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should receive WebRTC audio track and decode bytes on Wayland', async ({ page }) => {
    test.setTimeout(90000);

    page.on('console', msg => console.log('BROWSER:', msg.text()));

    await page.goto(`http://localhost:${PORT}`);

    // Wait for WebRTC connection
    await page.waitForFunction(() => {
        const statusEl = document.getElementById('status');
        return statusEl && statusEl.textContent && statusEl.textContent.includes('WebRTC');
    }, { timeout: 30000 });

    const status = await page.locator('#status').textContent();
    expect(status).toContain('WebRTC');

    // Give it a moment to settle any initial resizes
    await page.waitForTimeout(5000);

    // Interact to unmute and ensure audio context starts
    await page.click('body');
    await page.waitForTimeout(1000);

    console.log(`Spawning speaker-test inside Wayland container ${CONTAINER_NAME}...`);
    // Crucially set XDG_RUNTIME_DIR so speaker-test can connect to PulseAudio
    // Using -p 100 for continuous ping
    const aplayProc = spawn('docker', [
        'exec', 
        '--user', 'remote',
        '-e', 'XDG_RUNTIME_DIR=/tmp/llrdc-run', 
        CONTAINER_NAME, 
        'speaker-test', '-t', 'sine', '-f', '440', '-c', '2', '-l', '0'
    ]);

    aplayProc.stderr.on('data', (d) => console.log('speaker-test stderr:', d.toString()));

    // Wait for resizing/stabilization to finish before checking stats
    await page.waitForTimeout(10000);

    // Check the WebRTC stats for the audio track with polling directly
    let audioStats = { hasAudioTrack: false, bytesReceived: 0 };
    for (let i = 0; i < 30; i++) {
        audioStats = await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return { hasAudioTrack: false, bytesReceived: 0 };

            const stats = await rtcPeer.getStats();
            let audioBytes = 0;
            let hasAudio = false;
            
            // Total sum of inbound RTP
            let totalBytes = 0;
            let videoBytes = 0;

            stats.forEach(report => {
                if (report.type === 'inbound-rtp') {
                    const bytes = report.bytesReceived || 0;
                    totalBytes += bytes;
                    if (report.kind === 'video' || (report.framesDecoded !== undefined && report.framesDecoded > 0)) {
                        videoBytes = bytes;
                    } else if (report.kind === 'audio') {
                        hasAudio = true;
                        audioBytes = bytes;
                    }
                }
            });
            
            if (!hasAudio && totalBytes > videoBytes + 1000) {
                hasAudio = true;
                audioBytes = totalBytes - videoBytes;
            }

            return { hasAudioTrack: hasAudio, bytesReceived: audioBytes };
        });

        console.log(`WebRTC Audio Stats (Attempt ${i + 1}):`, audioStats);
        if (audioStats.hasAudioTrack && audioStats.bytesReceived > 1000) {
            break;
        }
        await page.waitForTimeout(1000);
    }

    if (aplayProc) {
        aplayProc.kill();
    }

    expect(audioStats.hasAudioTrack).toBe(true);
    expect(audioStats.bytesReceived).toBeGreaterThan(1000);

    // Open config menu
    await page.click('#config-btn');
    // Click Audio tab
    await page.click('[data-tab="tab-audio"]');

    // Test disabling audio
    console.log('Disabling audio...');
    await page.uncheck('#enable-audio-checkbox');
    
    // Wait for server to stop audio stream
    await page.waitForTimeout(4000);
    
    const getStatsBytes = async () => {
        return await page.evaluate(async () => {
            const rtcPeer = (window as any).rtcPeer as RTCPeerConnection;
            if (!rtcPeer) return 0;
            const stats = await rtcPeer.getStats();
            let ab = 0;
            let tb = 0;
            let vb = 0;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp') {
                    const bytes = report.bytesReceived || 0;
                    tb += bytes;
                    if (report.kind === 'video' || (report.framesDecoded !== undefined && report.framesDecoded > 0)) {
                        vb = bytes;
                    } else if (report.kind === 'audio') {
                        ab = bytes;
                    }
                }
            });
            if (ab === 0 && tb > vb + 1000) {
                ab = tb - vb;
            }
            return ab;
        });
    };
    
    const statsAfterDisable1 = await getStatsBytes();

    await page.waitForTimeout(2000);

    const statsAfterDisable2 = await getStatsBytes();
    
    console.log(`Bytes after disable check 1: ${statsAfterDisable1}, 2: ${statsAfterDisable2}`);
    // Bytes should stop increasing
    expect(statsAfterDisable2 - statsAfterDisable1).toBeLessThan(1000);

    // Test enabling audio again
    console.log('Re-enabling audio...');
    await page.check('#enable-audio-checkbox');
    await page.waitForTimeout(5000);

    const statsAfterEnable = await getStatsBytes();

    console.log(`Bytes after re-enable: ${statsAfterEnable}`);
    expect(statsAfterEnable).toBeGreaterThan(statsAfterDisable2);
  });
});
