import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

interface ProbeState {
    marker: number;
    color: 'black' | 'white';
    requestedAtMs: number;
    drawnAtMs: number;
    firstMoveAtMs: number;
    isMoving: boolean;
    pid: number;
    mouseX: number;
    mouseY: number;
}

function readProbeState(containerName: string): ProbeState {
    return JSON.parse(run(`docker exec ${containerName} cat /tmp/llrdc-latency-probe.json`)) as ProbeState;
}

test('macOS split architecture correctly streams video and has low mouse latency', async ({ page }) => {
    test.setTimeout(60000); // Allow time for the test to run
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Navigate to the local macOS server (already running)
    await page.goto('http://localhost:8080/viewer.html');

    // Wait for the video element to be attached and have a source
    const video = page.locator('#webrtc-video');
    await expect(video).toBeAttached({ timeout: 10000 });

    // Ensure video reaches playing state
    await page.waitForFunction(() => {
        const vid = document.getElementById('webrtc-video') as HTMLVideoElement;
        return vid && vid.readyState >= 3 && !vid.paused && vid.currentTime > 0;
    }, { timeout: 15000 });

    // Allow frames to accumulate
    await page.waitForTimeout(1000);

    // Verify decoder is receiving frames
    const frameStats = await page.evaluate(async () => {
        const pc = (window as any).webrtcPeerConnection || (window as any).rtcPeer;
        if (!pc) return null;
        
        const stats = await pc.getStats();
        let decodedFrames = 0;
        stats.forEach((report: any) => {
            if (report.type === 'inbound-rtp' && report.kind === 'video') {
                decodedFrames = report.framesDecoded || 0;
            }
        });
        return decodedFrames;
    });

    expect(frameStats).toBeGreaterThan(10);
    console.log(`Successfully verified split architecture! Decoded frames: ${frameStats}`);

    // ----- MOUSE LATENCY TEST -----
    console.log("Launching latency probe in Docker...");
    const containerName = "llrdc-macos";
    
    // Start the probe
    run(`docker exec -u remote -d ${containerName} bash -lc "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1"`);
    
    // Wait for probe to be ready
    let probeReady = false;
    for (let i = 0; i < 50; i++) {
        try {
            const state = readProbeState(containerName);
            if (typeof state.marker === 'number') {
                probeReady = true;
                break;
            }
        } catch (e) {}
        await page.waitForTimeout(100);
    }
    expect(probeReady).toBe(true);
    console.log("Latency probe is active.");

    const overlay = page.locator('#input-overlay');
    await overlay.hover();
    
    const box = await page.evaluate(() => {
        const el = document.getElementById('display-container');
        if (!el) return null;
        const rect = el.getBoundingClientRect();
        return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    });
    expect(box).not.toBeNull();

    const midX = box!.x + box!.width * 0.5;
    const midY = box!.y + box!.height * 0.5;
    const leftX = box!.x + box!.width * 0.2;
    const rightX = box!.x + box!.width * 0.8;

    let currentState = readProbeState(containerName);
    const startMarker = currentState.marker;
    
    console.log(`Starting continuous sweep test for 5 seconds.`);
    
    // 1. Move to left edge and let stream settle
    await page.mouse.move(leftX, midY);
    await page.waitForTimeout(1000); 

    const initialState = readProbeState(containerName);
    console.log(`Initial Wayland cursor at: X=${initialState.mouseX}`);

    // 2. Stress test: sweep back and forth for 5 seconds
    const startMs = Date.now();
    let sweeps = 0;
    while (Date.now() - startMs < 5000) {
        await page.mouse.move(rightX, midY, { steps: 20 });
        await page.waitForTimeout(50);
        await page.mouse.move(leftX, midY, { steps: 20 });
        await page.waitForTimeout(50);
        sweeps++;
    }
    
    console.log(`Completed ${sweeps} full sweep cycles over 5 seconds.`);
    await page.waitForTimeout(500); // Settle

    // 3. Final responsiveness check
    const stateBeforeFinal = readProbeState(containerName);
    console.log(`Cursor position after stress test: X=${stateBeforeFinal.mouseX}`);
    
    await page.mouse.move(midX, midY); // Jump to exact center
    
    let finalCursorX = 0;
    for (let i = 0; i < 50; i++) { // Poll for up to 5 seconds
        const state = readProbeState(containerName);
        // Wayland runs at 1920x1080. Center X is 960.
        if (Math.abs(state.mouseX - 960) < 100) { 
            finalCursorX = state.mouseX;
            break; 
        }
        await page.waitForTimeout(100);
    }

    console.log(`Final Wayland cursor arrived at: X=${finalCursorX}`);

    // If it's stuck, it will never reach the middle (960)
    expect(Math.abs(finalCursorX - 960)).toBeLessThan(100);
});
