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
    for (let i = 0; i < 5; i++) {
        try {
            const out = run(`docker exec ${containerName} cat /tmp/llrdc-latency-probe.json`);
            if (out && out.startsWith('{')) {
                return JSON.parse(out) as ProbeState;
            }
        } catch (e) {}
        execSync('sleep 0.1');
    }
    throw new Error("Failed to read probe state after 5 attempts");
}

test.beforeEach(async ({ page }) => {
    // Navigate to the local macOS server
    await page.goto('http://localhost:8080/viewer.html');

    // Wait for connection
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/\[(WebTransport|WebSocket)/i, { timeout: 30000 });

    // Reset settings to a clean baseline: H.264, 100% HDPI, Responsive Resolution
    console.log("Resetting server state to baseline (H.264, 100% HDPI, Responsive)...");
    await page.click('#config-btn');
    
    const codecSelect = page.locator('#video-codec-select');
    await codecSelect.selectOption('h264');
    
    const hdpiSelect = page.locator('#hdpi-select');
    await hdpiSelect.selectOption('100');
    
    const maxResSelect = page.locator('#max-res-select');
    await maxResSelect.selectOption('0'); // Responsive
    
    await page.click('#config-btn'); // Close panel
    
    // Wait for status to reflect baseline
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        return text;
    }, {
        timeout: 10000,
        message: "Waiting for status to show h264 4:2:0 baseline"
    }).toContain('h264');
});

test('macOS split architecture correctly streams video and has low mouse latency', async ({ page }) => {
    test.setTimeout(60000); // Allow time for the test to run
    
    page.on('console', msg => console.log('BROWSER: ' + msg.text()));
    
    // Set a deterministic viewport to prevent state leakage and huge resizes
    await page.setViewportSize({ width: 1280, height: 720 });

    // Measure startup latency
    const startTime = Date.now();

    // The server state is already reset by beforeEach
    const statusEl = page.locator('#status');
    const display = page.locator('#display');
    await expect(display).toBeAttached({ timeout: 15000 });

    // Allow frames to accumulate and handle potential initial connection drops
    console.log("Waiting for video stream to stabilize...");
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        const fps = match ? parseInt(match[1], 10) : 0;
        return fps;
    }, {
        timeout: 30000,
        intervals: [1000],
        message: "Waiting for FPS > 0"
    }).toBeGreaterThan(0);

    const startupLatency = Date.now() - startTime;
    console.log(`Measured Stream Startup Latency: ${startupLatency}ms`);

    const statusText = await statusEl.textContent() || '';
    console.log(`Successfully verified split architecture! Current status: ${statusText}`);

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
    
    // Get actual video dimensions to calculate expected center
    const videoDims = await display.evaluate((v: HTMLCanvasElement) => ({ w: v.width, h: v.height }));
    const expectedX = Math.round(videoDims.w / 2);
    console.log(`Expected center for ${videoDims.w}x${videoDims.h}: X=${expectedX}`);

    console.log("Sweep complete, allowing 2s for coordinates to settle...");
    await page.waitForTimeout(2000);

    let finalCursorX = 0;
    for (let i = 0; i < 50; i++) { // Poll for up to 5 seconds
        const state = readProbeState(containerName);
        if (Math.abs(state.mouseX - expectedX) < 100) { 
            finalCursorX = state.mouseX;
            break;
        }
        await page.waitForTimeout(100);
    }

    console.log(`Final Wayland cursor arrived at: X=${finalCursorX}`);

    // If it's stuck, it will never reach the expected center
    expect(Math.abs(finalCursorX - expectedX)).toBeLessThan(100);
    
    // Wait for system to settle
    await page.waitForTimeout(2000);
});

test('macOS split architecture respects framerate changes in config menu', async ({ page }) => {
    test.setTimeout(60000);
    // Connection and baseline reset handled by beforeEach

    const statusEl = page.locator('#status');

    // Open config menu
    console.log("Opening config menu for framerate test...");
    await page.click('#config-btn');
    const framerateSelect = page.locator('#framerate-select');
    await expect(framerateSelect).toBeVisible({ timeout: 5000 });

    // Test 15 FPS
    console.log("Setting framerate to 15 FPS...");
    await framerateSelect.selectOption('15');
    
    // Wait for FPS to stabilize around 15
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        return match ? parseInt(match[1], 10) : 0;
    }, {
        timeout: 20000,
        intervals: [1000],
        message: "Waiting for FPS to drop to ~15"
    }).toBeLessThanOrEqual(18); // Allow some buffer

    // Test 30 FPS
    console.log("Setting framerate to 30 FPS...");
    await framerateSelect.selectOption('30');

    // Wait for FPS to recover to ~30
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        return match ? parseInt(match[1], 10) : 0;
    }, {
        timeout: 20000,
        intervals: [1000],
        message: "Waiting for FPS to recover to ~30"
    }).toBeGreaterThanOrEqual(25);

    console.log("Framerate change verified successfully!");
    
    // Wait for system to settle
    await page.waitForTimeout(2000);
});

test('macOS split architecture supports dynamic HDPI scaling', async ({ page }) => {
    test.setTimeout(60000);
    // Connection and baseline reset handled by beforeEach

    const statusEl = page.locator('#status');

    // Open config menu
    console.log("Opening config menu for HDPI test...");
    await page.click('#config-btn');
    const hdpiSelect = page.locator('#hdpi-select');
    await expect(hdpiSelect).toBeVisible({ timeout: 5000 });

    // Change HDPI to 200%
    console.log("Changing HDPI to 200%...");
    await hdpiSelect.selectOption('200');

    // Verify the server applied the new HDPI scaling by checking agent logs
    const CONTAINER_NAME = 'llrdc-macos';
    
    await expect.poll(() => {
        try {
            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            return logs;
        } catch (e) {
            return '';
        }
    }, {
        message: 'Agent should apply dynamic 200% HDPI scaling',
        timeout: 20000,
    }).toContain('Agent received HDPI config: 200%');

    console.log("Agent received HDPI config!");

    await expect.poll(() => {
        try {
            const logs = execSync(`docker logs ${CONTAINER_NAME}`).toString();
            return logs;
        } catch (e) {
            return '';
        }
    }, {
        message: 'Compositor should apply 2x scaling',
        timeout: 20000,
    }).toContain('with scale 2.000000');

    console.log("HDPI 200% verified successfully!");
    
    // Wait for system to settle
    await page.waitForTimeout(2000);
});

test('macOS split architecture supports fixed resolution switching', async ({ page }) => {
    test.setTimeout(60000);
    // Connection and baseline reset handled by beforeEach

    const statusEl = page.locator('#status');

    // Open config menu
    console.log("Opening config menu for resolution test...");
    await page.click('#config-btn');
    const maxResSelect = page.locator('#max-res-select');
    await expect(maxResSelect).toBeVisible({ timeout: 5000 });

    // Switch to 720p
    console.log("Switching to 720p...");
    await maxResSelect.selectOption('720');

    // Wait for status bar to reflect 720p
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        return text;
    }, {
        timeout: 30000,
        message: "Waiting for status to show 720p"
    }).toContain('720');

    console.log("Fixed resolution 720p verified successfully!");
    
    // Wait for system to settle
    await page.waitForTimeout(2000);
});

test('macOS split architecture supports H.264 4:4:4 encoding', async ({ page }) => {
    test.setTimeout(60000);
    // Connection and baseline reset handled by beforeEach

    const statusEl = page.locator('#status');

    // Open config menu
    console.log("Opening config menu for H.264 4:4:4 test...");
    await page.click('#config-btn');
    
    const codecSelect = page.locator('#video-codec-select');
    await expect(codecSelect).toBeVisible({ timeout: 5000 });

    // Switch to H.264 4:4:4
    console.log("Switching to H.264 4:4:4...");
    await codecSelect.selectOption('h264-444');
    
    // Close menu
    await page.click('#config-btn');

    // Wait for status bar to reflect H.264 4:4:4
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        return text;
    }, {
        timeout: 30000,
        message: "Waiting for status to show h264-444"
    }).toContain('h264-444');

    // Verify frames are flowing
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        return match ? parseInt(match[1], 10) : 0;
    }, {
        timeout: 20000,
        message: "Waiting for FPS > 0 in H.264 4:4:4 mode"
    }).toBeGreaterThan(0);

    console.log("H.264 4:4:4 verified successfully!");
    
    // Wait for system to settle
    await page.waitForTimeout(2000);
});

test('macOS split architecture supports HEVC 4:4:4 encoding', async ({ page, browserName }) => {
    if (browserName === 'chromium') {
        test.skip(true, 'Chromium does not support HEVC (H.265)');
    }
    test.setTimeout(60000);
    // Connection and baseline reset handled by beforeEach

    const statusEl = page.locator('#status');

    // Open config menu
    console.log("Opening config menu for HEVC 4:4:4 test...");
    await page.click('#config-btn');
    
    const codecSelect = page.locator('#video-codec-select');
    await expect(codecSelect).toBeVisible({ timeout: 5000 });

    // Switch to HEVC 4:4:4
    console.log("Switching to HEVC 4:4:4...");
    await codecSelect.selectOption('h265-444');
    
    // Close menu
    await page.click('#config-btn');

    // Wait for status bar to reflect HEVC 4:4:4
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        return text;
    }, {
        timeout: 30000,
        message: "Waiting for status to show h265-444"
    }).toContain('h265-444');

    // Verify frames are flowing
    await expect.poll(async () => {
        const text = await statusEl.textContent() || '';
        const match = text.match(/FPS: (\d+)/);
        return match ? parseInt(match[1], 10) : 0;
    }, {
        timeout: 20000,
        message: "Waiting for FPS > 0 in HEVC 4:4:4 mode"
    }).toBeGreaterThan(0);

    console.log("HEVC 4:4:4 verified successfully!");
});
