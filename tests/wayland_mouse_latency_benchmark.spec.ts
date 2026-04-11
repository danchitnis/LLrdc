import { test, expect, type Locator, type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from './helpers';

type CaptureMode = 'compat' | 'direct';

interface FrameMetadataSample {
    brightness: number;
    callbackAtMs: number;
    expectedDisplayAtMs: number | null;
    presentationAtMs: number | null;
    captureAtMs: number | null;
    receiveAtMs: number | null;
    processingDurationMs: number | null;
    presentedFrames: number | null;
}

interface PresentedFrameSample extends FrameMetadataSample {
    matches: boolean;
}

interface ProbeState {
    marker: number;
    color: 'black' | 'white';
    requestedAtMs: number;
    drawnAtMs: number;
    firstMoveAtMs: number;
    isMoving: boolean;
    pid: number;
}

interface ServerLatencyTrace {
    marker: number;
    color: 'black' | 'white';
    requestedAtMs: number;
    drawnAtMs: number;
    firstFrameBroadcastAtMs: number;
}

interface BreakdownTrial {
    trial: number;
    color: 'black' | 'white';
    inputSentAtMs: number;
    requestedAtMs: number;
    drawnAtMs: number;
    firstMoveAtMs: number;
    serverTrace: ServerLatencyTrace;
    frame: FrameMetadataSample;
    stagesMs: {
        inputToFirstMove: number;
        firstMoveToRequest: number;
        inputToRequest: number;
        requestToDraw: number;
        drawToFirstFrameBroadcast: number | null;
        firstFrameBroadcastToReceive: number | null;
        receiveToDecodeReady: number | null;
        decodeReadyToCompose: number | null;
        composeToExpectedDisplay: number | null;
        expectedDisplayToCallback: number | null;
        drawToCallback: number;
        inputToCallback: number;
    };
}

interface BreakdownSummary {
    mode: CaptureMode;
    baseUrl: string;
    containerName: string;
    target: {
        videoCodec: string;
        fps: number;
        maxRes: number;
        bandwidthMbps: number;
        viewportWidth: number;
        viewportHeight: number;
    };
    observed: {
        streamWidth: number;
        streamHeight: number;
        statusText: string;
    };
    trials: BreakdownTrial[];
    averages: Record<string, number | null>;
}

interface BenchmarkResult {
    capturedAt: string;
    modes: BreakdownSummary[];
    delta?: Record<string, number | null>;
}

const TARGET_FPS = Number.parseInt(process.env.LLRDC_TARGET_FPS ?? '60', 10);
const TARGET_MAX_RES = Number.parseInt(process.env.LLRDC_TARGET_MAX_RES ?? '720', 10);
const TARGET_BANDWIDTH_MBPS = Number.parseInt(process.env.LLRDC_TARGET_BANDWIDTH_MBPS ?? '10', 10);
const TARGET_VIEWPORT_WIDTH = Number.parseInt(process.env.LLRDC_TARGET_VIEWPORT_WIDTH ?? '1280', 10);
const TARGET_VIEWPORT_HEIGHT = Number.parseInt(process.env.LLRDC_TARGET_VIEWPORT_HEIGHT ?? '720', 10);
const TARGET_VIDEO_CODEC = process.env.LLRDC_TARGET_VIDEO_CODEC ?? 'vp8';
const TARGET_USE_NVIDIA = (process.env.LLRDC_USE_NVIDIA ?? 'false') === 'true';
const TARGET_USE_INTEL = (process.env.LLRDC_USE_INTEL ?? 'false') === 'true';
const TARGET_CAPTURE_MODES = (process.env.LLRDC_CAPTURE_MODES ?? 'compat')
    .split(',')
    .map(mode => mode.trim())
    .filter((mode): mode is CaptureMode => mode === 'compat' || mode === 'direct');

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (_error) {}
}

async function startContainer(mode: CaptureMode, port: number, containerName: string): Promise<string> {
    killPort(port);
    try {
        execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });
    } catch (_error) {}

    const gpuArg = TARGET_USE_NVIDIA ? '--nvidia ' : '';
    const intelArg = TARGET_USE_INTEL ? '--intel ' : '';
    const debugArg = process.env.USE_DEBUG_FFMPEG === 'true' ? '--debug-ffmpeg ' : '';
    const resArg = TARGET_MAX_RES > 0 ? `--res ${TARGET_MAX_RES}p ` : '';
    execSync(`./docker-run.sh ${gpuArg}${intelArg}${debugArg}${resArg}--capture-mode ${mode} --detach --name ${containerName} --host-net`, {
        env: {
            ...process.env,
            PORT: port.toString(),
            HOST_PORT: port.toString(),
            CONTAINER_NAME: containerName,
            FPS: TARGET_FPS.toString(),
            BANDWIDTH: TARGET_BANDWIDTH_MBPS.toString(),
            VBR: 'false',
            ENABLE_AUDIO: 'false',
            VIDEO_CODEC: TARGET_VIDEO_CODEC,
        },
        stdio: 'inherit',
    });

    const baseUrl = `http://localhost:${port}`;
    await waitForServerReady(baseUrl, 60000);
    return baseUrl;
}

async function stopContainer(containerName: string, port: number) {
    killPort(port);
    try {
        execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });
    } catch (_error) {}
}

function readProbeState(containerName: string): ProbeState {
    return JSON.parse(run(`docker exec ${containerName} cat /tmp/llrdc-latency-probe.json`)) as ProbeState;
}

async function waitForProbeState(containerName: string): Promise<ProbeState> {
    const deadline = Date.now() + 20000;
    let lastError = '';

    while (Date.now() < deadline) {
        try {
            const state = readProbeState(containerName);
            if (typeof state.marker === 'number') {
                return state;
            }
        } catch (error) {
            lastError = error instanceof Error ? error.message : String(error);
        }
        await new Promise(resolve => setTimeout(resolve, 100));
    }

    throw new Error(`Timed out waiting for latency probe state in ${containerName}: ${lastError}`);
}

async function waitForDecodedFrames(page: Page, label: string) {
    await expect.poll(async () => {
        return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
        timeout: 45000,
        message: `Wait for decoded frames during ${label}`,
    }).toBeGreaterThan(0);
}

async function configureStreamTarget(page: Page, containerName: string) {
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();

    await page.locator('.config-tab-btn[data-tab="tab-stream"]').click();
    await page.selectOption('#max-res-select', TARGET_MAX_RES.toString());
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).not.toBeVisible();

    if (TARGET_MAX_RES > 0) {
        const minWidth = TARGET_MAX_RES >= 2160 ? 3200 : TARGET_MAX_RES >= 1440 ? 2200 : TARGET_MAX_RES >= 1080 ? 1600 : 1000;
        const minHeight = TARGET_MAX_RES >= 2160 ? 1800 : TARGET_MAX_RES >= 1440 ? 1200 : TARGET_MAX_RES >= 1080 ? 900 : 600;
        await waitForStreamResolution(page, minWidth, minHeight);
    }
}

async function setTargetViewport(page: Page) {
    await page.setViewportSize({
        width: TARGET_VIEWPORT_WIDTH,
        height: TARGET_VIEWPORT_HEIGHT,
    });
}

async function waitForStreamResolution(page: Page, minWidth: number, minHeight: number) {
    await expect.poll(async () => {
        const size = await page.evaluate(() => {
            const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;
            return {
                width: video?.videoWidth ?? 0,
                height: video?.videoHeight ?? 0,
            };
        });
        return size.width >= minWidth && size.height >= minHeight;
    }, {
        timeout: 45000,
        message: `Wait for stream resolution >= ${minWidth}x${minHeight}`,
    }).toBe(true);
}

async function initPresentedFrameTracker(page: Page) {
    await page.evaluate(() => {
        const win = window as Window & {
            __llrdcLatencyTrackerInstalled?: boolean;
            __llrdcLatestFrameMeta?: Omit<FrameMetadataSample, 'brightness' | 'callbackAtMs'> & { callbackAtMs: number };
        };
        if (win.__llrdcLatencyTrackerInstalled) {
            return;
        }

        const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;
        if (!video || typeof video.requestVideoFrameCallback !== 'function') {
            throw new Error('requestVideoFrameCallback is unavailable');
        }

        const toEpoch = (value: number | undefined) => typeof value === 'number' ? performance.timeOrigin + value : null;

        const update = (now: number, metadata: VideoFrameCallbackMetadata) => {
            win.__llrdcLatestFrameMeta = {
                callbackAtMs: performance.timeOrigin + now,
                expectedDisplayAtMs: toEpoch(metadata.expectedDisplayTime),
                presentationAtMs: toEpoch(metadata.presentationTime),
                captureAtMs: toEpoch((metadata as VideoFrameCallbackMetadata & { captureTime?: number }).captureTime),
                receiveAtMs: toEpoch((metadata as VideoFrameCallbackMetadata & { receiveTime?: number }).receiveTime),
                processingDurationMs: typeof metadata.processingDuration === 'number' ? metadata.processingDuration * 1000 : null,
                presentedFrames: typeof metadata.presentedFrames === 'number' ? metadata.presentedFrames : null,
            };
            video.requestVideoFrameCallback(update);
        };

        win.__llrdcLatencyTrackerInstalled = true;
        video.requestVideoFrameCallback(update);
    });
}

async function launchProbe(containerName: string) {
    run(`docker exec -u remote -d ${containerName} bash -lc "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1"`);
    await waitForProbeState(containerName);
}

async function waitForServerLatencyTrace(baseUrl: string, marker: number): Promise<ServerLatencyTrace> {
    const deadline = Date.now() + 10000;
    let lastStatus = 'trace not available yet';

    while (Date.now() < deadline) {
        try {
            const response = await fetch(`${baseUrl}/latencyz?marker=${marker}`);
            if (response.ok) {
                const trace = await response.json() as ServerLatencyTrace;
                if (trace.firstFrameBroadcastAtMs > 0) {
                    return trace;
                }
                lastStatus = JSON.stringify(trace);
            } else {
                lastStatus = await response.text();
            }
        } catch (error) {
            lastStatus = error instanceof Error ? error.message : String(error);
        }
        await new Promise(resolve => setTimeout(resolve, 50));
    }

    throw new Error(`Timed out waiting for latency trace for marker ${marker}: ${lastStatus}`);
}

async function sweepUntilProbeToggles(
    page: Page,
    containerName: string,
    previousMarker: number,
    sweepDirection: 'left-to-right' | 'right-to-left'
): Promise<{ state: ProbeState; inputSentAtMs: number }> {
    const box = await page.evaluate(() => {
        const el = document.getElementById('display-container');
        if (!el) return null;
        const rect = el.getBoundingClientRect();
        return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    });
    if (!box) throw new Error('Could not find display container bounding box');

    const midX = box.x + box.width * 0.5;
    const midY = box.y + box.height * 0.5;
    
    // Fast hop across the center (±50 pixels)
    const leftX = midX - 50;
    const rightX = midX + 50;

    for (let attempt = 1; attempt <= 3; attempt++) {
        const startX = sweepDirection === 'left-to-right' ? leftX : rightX;
        const endX = sweepDirection === 'left-to-right' ? rightX : leftX;

        // Move to start position first and let the stream settle
        await page.mouse.move(startX, midY);
        await page.waitForTimeout(200);

        const inputSentAtMs = Date.now();
        // Instantaneous sweep across the middle to eliminate Playwright's event loop stepping delays
        await page.mouse.move(endX, midY, { steps: 5 });

        try {
            await expect.poll(() => readProbeState(containerName).marker, {
                timeout: 3000,
                message: `Wait for probe marker > ${previousMarker} in ${containerName} (attempt ${attempt})`,
            }).toBeGreaterThan(previousMarker);

            const finalState = readProbeState(containerName);
            return {
                state: finalState,
                inputSentAtMs,
            };
        } catch (_error) {
            console.log(`Attempt ${attempt} failed to trigger marker increment. Last marker: ${readProbeState(containerName).marker}`);
            await page.waitForTimeout(500);
        }
    }

    throw new Error(`Probe marker did not advance in ${containerName}; last marker=${readProbeState(containerName).marker}`);
}

async function waitForPresentedFrameColor(page: Page, expectedColor: 'black' | 'white'): Promise<PresentedFrameSample> {
    return await page.evaluate(({ expected }) => {
        return new Promise<PresentedFrameSample>((resolve, reject) => {
            const canvas = document.getElementById('display') as HTMLCanvasElement | null;
            const ctx = canvas?.getContext('2d', { willReadFrequently: true });
            if (!ctx) {
                reject(new Error('Failed to access display canvas'));
                return;
            }

            const deadline = performance.now() + 10000;
            const win = window as Window & {
                __llrdcLatestFrameMeta?: Omit<FrameMetadataSample, 'brightness' | 'callbackAtMs'> & { callbackAtMs: number };
            };

            const sample = () => {
                if (!canvas || canvas.width <= 0 || canvas.height <= 0) {
                    if (performance.now() > deadline) {
                        reject(new Error(`Timed out waiting for display dimensions for ${expected}`));
                        return;
                    }
                    requestAnimationFrame(sample);
                    return;
                }

                const cx = Math.floor(canvas.width / 2);
                const cy = Math.floor(canvas.height / 2);
                const radius = 20;
                const image = ctx.getImageData(cx - radius, cy - radius, radius * 2, radius * 2).data;
                let total = 0;
                let count = 0;
                for (let i = 0; i < image.length; i += 4) {
                    total += (image[i] + image[i + 1] + image[i + 2]) / 3;
                    count++;
                }

                const brightness = count > 0 ? total / count : -1;
                const matches = expected === 'white' ? brightness >= 200 : brightness <= 55;

                if (matches) {
                    const nowEpoch = performance.timeOrigin + performance.now();
                    const latest = win.__llrdcLatestFrameMeta;
                    resolve({
                        matches,
                        brightness,
                        callbackAtMs: nowEpoch,
                        expectedDisplayAtMs: latest?.expectedDisplayAtMs ?? nowEpoch,
                        presentationAtMs: latest?.presentationAtMs ?? null,
                        captureAtMs: latest?.captureAtMs ?? null,
                        receiveAtMs: latest?.receiveAtMs ?? null,
                        processingDurationMs: latest?.processingDurationMs ?? null,
                        presentedFrames: latest?.presentedFrames ?? null,
                    });
                    return;
                }

                if (performance.now() > deadline) {
                    reject(new Error(`Timed out waiting for displayed frame to turn ${expected} (current brightness: ${brightness})`));
                    return;
                }

                requestAnimationFrame(sample);
            };

            requestAnimationFrame(sample);
        });
    }, { expected: expectedColor });
}

function average(values: Array<number | null>): number | null {
    const usable = values.filter((value): value is number => typeof value === 'number' && Number.isFinite(value));
    if (usable.length === 0) {
        return null;
    }
    return usable.reduce((sum, value) => sum + value, 0) / usable.length;
}

function buildStageBreakdown(inputSentAtMs: number, probe: ProbeState, serverTrace: ServerLatencyTrace, frame: FrameMetadataSample) {
    const decodeReadyAtMs = frame.receiveAtMs !== null && frame.processingDurationMs !== null
        ? frame.receiveAtMs + frame.processingDurationMs
        : null;

    return {
        inputToFirstMove: probe.firstMoveAtMs - inputSentAtMs,
        firstMoveToRequest: probe.requestedAtMs - probe.firstMoveAtMs,
        inputToRequest: probe.requestedAtMs - inputSentAtMs,
        requestToDraw: probe.drawnAtMs - probe.requestedAtMs,
        drawToFirstFrameBroadcast: serverTrace.firstFrameBroadcastAtMs > 0 ? serverTrace.firstFrameBroadcastAtMs - probe.drawnAtMs : null,
        firstFrameBroadcastToReceive: serverTrace.firstFrameBroadcastAtMs > 0 && frame.receiveAtMs !== null ? frame.receiveAtMs - serverTrace.firstFrameBroadcastAtMs : null,
        receiveToDecodeReady: frame.processingDurationMs,
        decodeReadyToCompose: decodeReadyAtMs !== null && frame.presentationAtMs !== null ? frame.presentationAtMs - decodeReadyAtMs : null,
        composeToExpectedDisplay: frame.presentationAtMs !== null && frame.expectedDisplayAtMs !== null ? frame.expectedDisplayAtMs - frame.presentationAtMs : null,
        expectedDisplayToCallback: frame.expectedDisplayAtMs !== null ? frame.callbackAtMs - frame.expectedDisplayAtMs : null,
        drawToCallback: frame.callbackAtMs - probe.drawnAtMs,
        inputToCallback: frame.callbackAtMs - inputSentAtMs,
    };
}

async function collectModeSummary(
    page: Page,
    mode: CaptureMode,
    baseUrl: string,
    containerName: string,
): Promise<BreakdownSummary> {
    const ready = await fetchReadyz(baseUrl);
    if (mode === 'direct') {
        expect(ready.directBuffer?.active).toBe(true);
    } else {
        expect(ready.directBuffer?.active).not.toBe(true);
    }

    await page.goto(baseUrl);
    await page.click('body');
    await expect(page.locator('#status')).toContainText(/\[.*\]/, { timeout: 45000 });
    await setTargetViewport(page);
    await configureStreamTarget(page, containerName);
    await initPresentedFrameTracker(page);
    await waitForDecodedFrames(page, `${mode} initial stream`);
    await waitForDecodedFrames(page, `${mode} configured stream`);
    if (TARGET_MAX_RES > 0) {
        const minWidth = TARGET_MAX_RES >= 2160 ? 3200 : TARGET_MAX_RES >= 1440 ? 2200 : TARGET_MAX_RES >= 1080 ? 1600 : 1000;
        const minHeight = TARGET_MAX_RES >= 2160 ? 1800 : TARGET_MAX_RES >= 1440 ? 1200 : TARGET_MAX_RES >= 1080 ? 900 : 600;
        await waitForStreamResolution(page, minWidth, minHeight);
    }
    await launchProbe(containerName);

    // Inject a visual tracker so we can see Playwright's synthetic mouse movements
    await page.evaluate(() => {
        const dot = document.createElement('div');
        dot.id = 'playwright-cursor-tracker';
        dot.style.position = 'fixed';
        dot.style.width = '8px';
        dot.style.height = '8px';
        dot.style.backgroundColor = 'red';
        dot.style.borderRadius = '50%';
        dot.style.pointerEvents = 'none';
        dot.style.zIndex = '999999';
        dot.style.transform = 'translate(-50%, -50%)';
        dot.style.boxShadow = '0 0 4px rgba(0,0,0,0.5)';
        document.body.appendChild(dot);

        document.addEventListener('mousemove', (e) => {
            dot.style.left = `${e.clientX}px`;
            dot.style.top = `${e.clientY}px`;
        });
    });

    const overlay = page.locator('#input-overlay');
    await expect(overlay).toBeVisible();
    await overlay.hover();

    let state = readProbeState(containerName);
    
    // warmup
    for (let i = 0; i < 2; i++) {
        const direction = i % 2 === 0 ? 'left-to-right' : 'right-to-left';
        const toggle = await sweepUntilProbeToggles(page, containerName, state.marker, direction);
        state = toggle.state;
        await waitForPresentedFrameColor(page, state.color);
        await page.waitForTimeout(150);
    }

    const trials: BreakdownTrial[] = [];

    for (let trial = 1; trial <= 10; trial++) {
        const expectedColor = state.color === 'black' ? 'white' : 'black';
        const direction = trial % 2 === 0 ? 'left-to-right' : 'right-to-left';
        
        // Start waiting for the color BEFORE the sweep blocks the event loop
        const framePromise = waitForPresentedFrameColor(page, expectedColor);
        
        const toggle = await sweepUntilProbeToggles(page, containerName, state.marker, direction);
        state = toggle.state;
        
        const [serverTrace, frame] = await Promise.all([
            waitForServerLatencyTrace(baseUrl, state.marker),
            framePromise,
        ]);
        const stagesMs = buildStageBreakdown(toggle.inputSentAtMs, state, serverTrace, frame);

        trials.push({
            trial,
            color: state.color,
            inputSentAtMs: toggle.inputSentAtMs,
            requestedAtMs: state.requestedAtMs,
            drawnAtMs: state.drawnAtMs,
            firstMoveAtMs: state.firstMoveAtMs,
            serverTrace,
            frame,
            stagesMs,
        });

        await page.waitForTimeout(300);
    }

    const averages = {
        inputToFirstMove: average(trials.map(trial => trial.stagesMs.inputToFirstMove)),
        firstMoveToRequest: average(trials.map(trial => trial.stagesMs.firstMoveToRequest)),
        inputToRequest: average(trials.map(trial => trial.stagesMs.inputToRequest)),
        requestToDraw: average(trials.map(trial => trial.stagesMs.requestToDraw)),
        drawToFirstFrameBroadcast: average(trials.map(trial => trial.stagesMs.drawToFirstFrameBroadcast)),
        firstFrameBroadcastToReceive: average(trials.map(trial => trial.stagesMs.firstFrameBroadcastToReceive)),
        receiveToDecodeReady: average(trials.map(trial => trial.stagesMs.receiveToDecodeReady)),
        decodeReadyToCompose: average(trials.map(trial => trial.stagesMs.decodeReadyToCompose)),
        composeToExpectedDisplay: average(trials.map(trial => trial.stagesMs.composeToExpectedDisplay)),
        expectedDisplayToCallback: average(trials.map(trial => trial.stagesMs.expectedDisplayToCallback)),
        drawToCallback: average(trials.map(trial => trial.stagesMs.drawToCallback)),
        inputToCallback: average(trials.map(trial => trial.stagesMs.inputToCallback)),
    };

    const observed = await page.evaluate(() => {
        const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;
        const status = document.getElementById('status') as HTMLDivElement | null;
        return {
            streamWidth: video?.videoWidth ?? 0,
            streamHeight: video?.videoHeight ?? 0,
            statusText: status?.textContent ?? '',
        };
    });

    return {
        mode,
        baseUrl,
        containerName,
        target: {
            videoCodec: TARGET_VIDEO_CODEC,
            fps: TARGET_FPS,
            maxRes: TARGET_MAX_RES,
            bandwidthMbps: TARGET_BANDWIDTH_MBPS,
            viewportWidth: TARGET_VIEWPORT_WIDTH,
            viewportHeight: TARGET_VIEWPORT_HEIGHT,
        },
        observed,
        trials,
        averages,
    };
}

test.use({ headless: false }); // Specifically ensure headed for this test to observe mouse movements

test.describe('Wayland Mouse Latency Benchmark', () => {
    test.describe.configure({ retries: 2 });

    test('should break down latency of mouse movement', async ({ browser }, testInfo) => {
        test.setTimeout(360000);

        expect(TARGET_CAPTURE_MODES.length).toBeGreaterThan(0);

        const basePort = 8100 + Math.floor(Math.random() * 800);
        const ports = new Map<CaptureMode, number>();
        const containers = new Map<CaptureMode, string>();
        TARGET_CAPTURE_MODES.forEach((mode, index) => {
            ports.set(mode, basePort + index);
            containers.set(mode, `llrdc-mouse-latency-${mode}`);
        });

        const summaries = new Map<CaptureMode, BreakdownSummary>();

        try {
            for (const mode of TARGET_CAPTURE_MODES) {
                const port = ports.get(mode)!;
                const container = containers.get(mode)!;
                const url = await startContainer(mode, port, container);
                const page = await browser.newPage();
                const summary = await collectModeSummary(page, mode, url, container);
                summaries.set(mode, summary);
                await page.close();
                await stopContainer(container, port);
            }
        } finally {
            // Cleanup any remaining containers in case of failure
            for (const mode of TARGET_CAPTURE_MODES) {
                try {
                    execSync(`docker rm -f ${containers.get(mode)!}`, { stdio: 'ignore' });
                } catch (e) {}
            }
        }

        for (const mode of TARGET_CAPTURE_MODES) {
            expect(summaries.get(mode)).toBeDefined();
        }

        let delta: Record<string, number | null> | undefined;
        if (summaries.has('compat') && summaries.has('direct')) {
            const compatSummary = summaries.get('compat')!;
            const directSummary = summaries.get('direct')!;
            const stageNames = Object.keys(compatSummary.averages);
            delta = Object.fromEntries(stageNames.map(stage => {
                const compat = compatSummary.averages[stage];
                const direct = directSummary.averages[stage];
                return [stage, typeof compat === 'number' && typeof direct === 'number' ? direct - compat : null];
            }));
        }

        const result: BenchmarkResult = {
            capturedAt: new Date().toISOString(),
            modes: TARGET_CAPTURE_MODES.map(mode => summaries.get(mode)!),
            ...(delta ? { delta } : {}),
        };

        console.log('Mouse Latency benchmark summary:');
        console.log(JSON.stringify(result, null, 2));

        await testInfo.attach('mouse-latency-benchmark', {
            body: Buffer.from(JSON.stringify(result, null, 2)),
            contentType: 'application/json',
        });
    });
});
