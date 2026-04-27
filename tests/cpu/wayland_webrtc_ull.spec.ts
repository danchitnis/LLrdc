import { test, expect, type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { mkdirSync, writeFileSync } from 'fs';
import { join } from 'path';
import { waitForServerReady } from '../helpers';

test.use({ headless: false });

type LatencyMode = 'standard' | 'ull';

interface ProbeState {
    marker: number;
    color: 'black' | 'white';
    requestedAtMs: number;
    drawnAtMs: number;
}

interface FrameMetadataSample {
    callbackAtMs: number;
    expectedDisplayAtMs: number | null;
    presentationAtMs: number | null;
    captureAtMs: number | null;
    receiveAtMs: number | null;
    processingDurationMs: number | null;
    presentedFrames: number | null;
    rtpTimestamp?: number;
}

interface PresentedFrameSample extends FrameMetadataSample {
    brightness: number;
    markerCode: number;
    colorMatches: boolean;
    markerMatches: boolean;
    matchMethod: 'rtp-timestamp' | 'visible-marker';
    rtpTimestampMatches: boolean;
}

interface ServerLatencyTrace {
    marker: number;
    serverTimeMs: number;
    requestedAtMs: number;
    drawnAtMs: number;
    firstFrameBroadcastAtMs: number;
    firstPacketTimestamp: number;
}

interface ClockSync {
    offsetMs: number;
    rttMs: number;
    samples: Array<{ offsetMs: number; rttMs: number; serverTimeMs: number }>;
}

interface ReceiverHintSnapshot {
    playoutDelayHintSupported: boolean;
    playoutDelayHint?: number | null;
    jitterBufferTargetSupported: boolean;
    jitterBufferTarget?: number | null;
}

interface BrowserModeState {
    lowLatencyMode: boolean;
    checkboxChecked: boolean | null;
    receiverHints: ReceiverHintSnapshot[];
}

interface StageBreakdown {
    inputToRemoteRequestMs: number;
    remoteRequestToDrawMs: number;
    remoteDrawToFirstFrameBroadcastMs: number | null;
    firstFrameBroadcastToReceiveMs: number | null;
    receiveToDecodeReadyMs: number | null;
    decodeReadyToComposeMs: number | null;
    composeToExpectedDisplayMs: number | null;
    expectedDisplayToCallbackMs: number | null;
    remoteDrawToBrowserCallbackMs: number;
    inputToBrowserCallbackMs: number;
}

interface BreakdownTrial {
    trial: number;
    marker: number;
    color: 'black' | 'white';
    inputSentAtMs: number;
    requestedAtMs: number;
    drawnAtMs: number;
    markerCodeSaturated: boolean;
    clockSync: Pick<ClockSync, 'offsetMs' | 'rttMs'>;
    serverTrace: ServerLatencyTrace;
    frame: PresentedFrameSample;
    stagesMs: StageBreakdown;
}

interface StageStats {
    count: number;
    min: number | null;
    p10: number | null;
    median: number | null;
    mean: number | null;
    p90: number | null;
    max: number | null;
}

interface BreakdownSummary {
    mode: LatencyMode;
    baseUrl: string;
    containerName: string;
    target: {
        videoCodec: string;
        fps: number;
        maxRes: number;
        bandwidthMbps: number;
        viewportWidth: number;
        viewportHeight: number;
        trials: number;
        warmupTrials: number;
        minValidTrials: number;
    };
    observed: {
        streamWidth: number;
        streamHeight: number;
        statusText: string;
        totalDecoded: number;
        jitterBufferDelay: number | null;
        jitterBufferTarget: number | null;
        browserMode: BrowserModeState;
        clockSync: ClockSync;
    };
    trials: BreakdownTrial[];
    stageStats: Record<keyof StageBreakdown, StageStats>;
}

interface BenchmarkResult {
    capturedAt: string;
    modes: BreakdownSummary[];
    deltaMedian: Partial<Record<keyof StageBreakdown, number | null>>;
}

const TARGET_FPS = Number.parseInt(process.env.LLRDC_TARGET_FPS ?? '60', 10);
const TARGET_MAX_RES = Number.parseInt(process.env.LLRDC_TARGET_MAX_RES ?? '720', 10);
const TARGET_BANDWIDTH_MBPS = Number.parseInt(process.env.LLRDC_TARGET_BANDWIDTH_MBPS ?? '10', 10);
const TARGET_VIEWPORT_WIDTH = Number.parseInt(process.env.LLRDC_TARGET_VIEWPORT_WIDTH ?? '1280', 10);
const TARGET_VIEWPORT_HEIGHT = Number.parseInt(process.env.LLRDC_TARGET_VIEWPORT_HEIGHT ?? '720', 10);
const TARGET_VIDEO_CODEC = process.env.LLRDC_TARGET_VIDEO_CODEC ?? 'vp8';
const WARMUP_TRIALS = Number.parseInt(process.env.LLRDC_ULL_WARMUP_TRIALS ?? '5', 10);
const MEASURED_TRIALS = Number.parseInt(process.env.LLRDC_ULL_TRIALS ?? '30', 10);
const MIN_VALID_TRIALS = Number.parseInt(process.env.LLRDC_ULL_MIN_VALID_TRIALS ?? Math.min(25, MEASURED_TRIALS).toString(), 10);
const CLOCK_SYNC_SAMPLES = Number.parseInt(process.env.LLRDC_CLOCK_SYNC_SAMPLES ?? '9', 10);
const CLOCK_SYNC_MAX_RTT_MS = Number.parseFloat(process.env.LLRDC_CLOCK_SYNC_MAX_RTT_MS ?? '30');
const ASSERT_ULL_IMPROVES = process.env.LLRDC_ASSERT_ULL_IMPROVES === 'true';
const ASSERT_ULL_IMPROVEMENT_MS = Number.parseFloat(process.env.LLRDC_ASSERT_ULL_IMPROVEMENT_MS ?? '8');

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

function sleepSync(ms: number) {
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (_error) {}
}

async function startContainer(mode: LatencyMode, port: number, containerName: string): Promise<string> {
    killPort(port);
    try {
        execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });
    } catch (_error) {}

    const ullArgs = mode === 'ull' ? '--webrtc-low-latency --webrtc-buffer 0 ' : '';
    execSync(`./docker-run.sh ${ullArgs}--res ${TARGET_MAX_RES}p --capture-mode compat --detach --name ${containerName} --host-net`, {
        env: {
            ...process.env,
            PORT: port.toString(),
            HOST_PORT: port.toString(),
            CONTAINER_NAME: containerName,
            FPS: TARGET_FPS.toString(),
            BANDWIDTH: TARGET_BANDWIDTH_MBPS.toString(),
            VBR: 'false',
            DAMAGE_TRACKING: 'false',
            ENABLE_AUDIO: 'false',
            VIDEO_CODEC: TARGET_VIDEO_CODEC,
            USE_NVIDIA: 'false',
            USE_INTEL: 'false',
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
    let lastError = '';
    for (let attempt = 0; attempt < 20; attempt++) {
        const out = run(`docker exec ${containerName} cat /tmp/llrdc-latency-probe.json`);
        try {
            if (out.length === 0) throw new Error('empty probe state');
            return JSON.parse(out) as ProbeState;
        } catch (error) {
            lastError = error instanceof Error ? error.message : String(error);
            sleepSync(10);
        }
    }
    throw new Error(`Failed to read stable latency probe state from ${containerName}: ${lastError}`);
}

async function waitForProbeState(containerName: string): Promise<ProbeState> {
    const deadline = Date.now() + 30000;
    let lastError = '';
    while (Date.now() < deadline) {
        try {
            const state = readProbeState(containerName);
            if (typeof state.marker === 'number' && state.drawnAtMs > 0) return state;
        } catch (error) {
            lastError = error instanceof Error ? error.message : String(error);
        }
        await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error(`Timed out waiting for latency probe state in ${containerName}: ${lastError}`);
}

async function waitForDecodedFrames(page: Page, label: string) {
    await expect.poll(async () => {
        const before = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
        await page.waitForTimeout(1000);
        const after = await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
        return after > before && after > 0;
    }, {
        timeout: 45000,
        message: `Wait for decoded frames during ${label}`,
    }).toBe(true);
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
                if (trace.firstFrameBroadcastAtMs > 0 && trace.firstPacketTimestamp > 0) return trace;
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

async function syncBrowserToServerClock(page: Page, baseUrl: string): Promise<ClockSync> {
    const sync = await page.evaluate(async ({ url, samples }) => {
        const collected: Array<{ offsetMs: number; rttMs: number; serverTimeMs: number }> = [];
        for (let i = 0; i < samples; i++) {
            const start = performance.now();
            const response = await fetch(`${url}/timez`, { cache: 'no-store' });
            const body = await response.json() as { serverTimeMs: number };
            const end = performance.now();
            const rttMs = end - start;
            const midpoint = start + (rttMs / 2);
            collected.push({
                offsetMs: midpoint - body.serverTimeMs,
                rttMs,
                serverTimeMs: body.serverTimeMs,
            });
            await new Promise(resolve => setTimeout(resolve, 25));
        }
        const best = collected.reduce((current, candidate) => candidate.rttMs < current.rttMs ? candidate : current);
        return {
            offsetMs: best.offsetMs,
            rttMs: best.rttMs,
            samples: collected,
        };
    }, { url: baseUrl, samples: CLOCK_SYNC_SAMPLES });

    expect(sync.rttMs, `Best /timez clock sync RTT for ${baseUrl}`).toBeLessThanOrEqual(CLOCK_SYNC_MAX_RTT_MS);
    return sync;
}

async function initPresentedFrameTracker(page: Page) {
    await page.evaluate(() => {
        const client = window.__llrdcClient;
        if (!client?.getPresentedFrames) {
            throw new Error('Presented frame hook is unavailable');
        }
        client.clearPresentedFrames?.();
    });
}

async function readPresentedFrameCursor(page: Page): Promise<{ callbackAtMs: number; presentedFrames: number }> {
    return await page.evaluate(() => {
        const frames = window.__llrdcClient?.getPresentedFrames?.() ?? [];
        const last = frames[frames.length - 1];
        return {
            callbackAtMs: typeof last?.callbackAtMs === 'number' ? last.callbackAtMs : 0,
            presentedFrames: typeof last?.presentedFrames === 'number' ? last.presentedFrames : 0,
        };
    });
}

async function moveProbeOutside(page: Page, containerName: string) {
    const box = await page.locator('#input-overlay').boundingBox();
    if (!box) throw new Error('Input overlay is not visible');
    await page.mouse.move(box.x + box.width * 0.12, box.y + box.height * 0.12, { steps: 2 });
    await expect.poll(() => readProbeState(containerName).color, {
        timeout: 3000,
        message: `Wait for probe to return to black in ${containerName}`,
    }).toBe('black');
}

async function triggerProbe(page: Page, containerName: string, previousMarker: number): Promise<{ state: ProbeState; inputSentAtMs: number }> {
    const box = await page.locator('#input-overlay').boundingBox();
    if (!box) throw new Error('Input overlay is not visible');
    let lastMarker = previousMarker;
    let inputSentAtMs = 0;
    for (let attempt = 1; attempt <= 5; attempt++) {
        await page.mouse.move(box.x + box.width * 0.12, box.y + box.height * 0.12, { steps: 2 });
        await page.waitForTimeout(200);
        inputSentAtMs = await page.evaluate(() => performance.now());
        await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.5, { steps: 2 });
        await page.mouse.down();
        await page.mouse.up();

        try {
            await expect.poll(() => readProbeState(containerName).marker, {
                timeout: 1500,
                message: `Wait for probe marker > ${previousMarker} in ${containerName} (attempt ${attempt})`,
            }).toBeGreaterThan(previousMarker);
            const state = readProbeState(containerName);
            expect(state.color, `Triggered marker ${state.marker} should be white`).toBe('white');
            return { state, inputSentAtMs };
        } catch (_error) {
            lastMarker = readProbeState(containerName).marker;
        }
    }
    throw new Error(`Probe marker did not advance past ${previousMarker} in ${containerName}; last marker=${lastMarker}`);
}

async function waitForVisibleProbeState(page: Page, expectedColor: 'black' | 'white', expectedMarker: number, minPresentedFrames: number) {
    await page.evaluate(({ color, marker, minFrames }) => {
        return new Promise<void>((resolve, reject) => {
            const canvas = document.getElementById('display') as HTMLCanvasElement | null;
            const ctx = canvas?.getContext('2d', { willReadFrequently: true });
            const deadline = performance.now() + 10000;
            const expectedMarkerCode = Math.min(marker, 16);

            const sampleCanvas = () => {
                if (!canvas || !ctx || canvas.width <= 0 || canvas.height <= 0) {
                    if (performance.now() > deadline) reject(new Error('Timed out waiting for display canvas'));
                    else requestAnimationFrame(sampleCanvas);
                    return;
                }

                const frames = window.__llrdcClient?.getPresentedFrames?.() ?? [];
                const latest = frames[frames.length - 1];
                if (typeof latest?.presentedFrames === 'number' && latest.presentedFrames <= minFrames) {
                    if (performance.now() > deadline) reject(new Error('Timed out waiting for a fresh presented frame'));
                    else requestAnimationFrame(sampleCanvas);
                    return;
                }

                const visible = readVisibleProbeState(canvas, ctx);
                if (visible.colorMatches === (color === 'white') && visible.markerCode === expectedMarkerCode) {
                    resolve();
                    return;
                }

                if (performance.now() > deadline) {
                    reject(new Error(`Timed out waiting for visible ${color} marker ${expectedMarkerCode}; last=${JSON.stringify(visible)}`));
                    return;
                }
                requestAnimationFrame(sampleCanvas);
            };

            sampleCanvas();
        });

        function readVisibleProbeState(canvas: HTMLCanvasElement, ctx: CanvasRenderingContext2D) {
            const cx = Math.floor(canvas.width / 2);
            const cy = Math.floor(canvas.height / 2);
            const radius = 20;
            const center = ctx.getImageData(cx - radius, cy - radius, radius * 2, radius * 2).data;
            let brightnessTotal = 0;
            let brightnessCount = 0;
            for (let i = 0; i < center.length; i += 4) {
                brightnessTotal += (center[i] + center[i + 1] + center[i + 2]) / 3;
                brightnessCount++;
            }
            const brightness = brightnessCount > 0 ? brightnessTotal / brightnessCount : -1;
            return {
                brightness,
                colorMatches: brightness >= 200,
                markerCode: readMarkerCode(ctx),
            };
        }

        function readMarkerCode(ctx: CanvasRenderingContext2D): number {
            const markerStartX = 120;
            const markerStartY = 40;
            const markerCellSize = 20;
            const markerCellGap = 10;
            let code = 0;
            for (let bit = 0; bit < 16; bit++) {
                const x = markerStartX + bit * (markerCellSize + markerCellGap);
                const brightness = averageCellBrightness(ctx, x, markerStartY, markerCellSize);
                if (brightness < 120) code++;
                else break;
            }
            return code;
        }

        function averageCellBrightness(ctx: CanvasRenderingContext2D, x: number, y: number, size: number): number {
            const data = ctx.getImageData(x, y, size, size).data;
            let total = 0;
            let count = 0;
            for (let i = 0; i < data.length; i += 4) {
                total += (data[i] + data[i + 1] + data[i + 2]) / 3;
                count++;
            }
            return count > 0 ? total / count : 255;
        }
    }, { color: expectedColor, marker: expectedMarker, minFrames: minPresentedFrames });
}

async function waitForPresentedFrameMatch(
    page: Page,
    trace: ServerLatencyTrace,
    earliestInputNowMs: number,
    minPresentedFrames: number,
    allowVisualFallback: boolean,
): Promise<PresentedFrameSample> {
    return await page.evaluate(({ marker, rtpTimestamp, earliestNow, minFrames, allowFallback }) => {
        return new Promise<PresentedFrameSample>((resolve, reject) => {
            const canvas = document.getElementById('display') as HTMLCanvasElement | null;
            const ctx = canvas?.getContext('2d', { willReadFrequently: true });
            const deadline = performance.now() + 10000;
            const expectedMarkerCode = Math.min(marker, 16);
            const earliestCallbackAtMs = performance.timeOrigin + earliestNow;

            const sample = () => {
                if (!canvas || !ctx || canvas.width <= 0 || canvas.height <= 0) {
                    if (performance.now() > deadline) reject(new Error('Timed out waiting for display canvas'));
                    else requestAnimationFrame(sample);
                    return;
                }

                const frames = window.__llrdcClient?.getPresentedFrames?.() ?? [];
                const candidates = frames.filter(frame => (
                    frame.callbackAtMs >= earliestCallbackAtMs &&
                    (typeof frame.presentedFrames !== 'number' || frame.presentedFrames > minFrames)
                ));
                const matched = candidates.find(frame => (
                    frame.callbackAtMs >= earliestCallbackAtMs &&
                    frame.rtpTimestamp === rtpTimestamp &&
                    (typeof frame.presentedFrames !== 'number' || frame.presentedFrames > minFrames)
                ));
                const visible = readVisibleProbeState(canvas, ctx, expectedMarkerCode);
                const fallback = candidates[candidates.length - 1];

                if ((matched || (allowFallback && fallback)) && visible.colorMatches && visible.markerMatches) {
                    const frame = matched ?? fallback;
                    resolve({
                        ...frame,
                        brightness: visible.brightness,
                        markerCode: visible.markerCode,
                        colorMatches: visible.colorMatches,
                        markerMatches: visible.markerMatches,
                        matchMethod: matched ? 'rtp-timestamp' : 'visible-marker',
                        rtpTimestampMatches: Boolean(matched),
                    });
                    return;
                }

                if (performance.now() > deadline) {
                    reject(new Error(`Timed out waiting for visible marker ${expectedMarkerCode}; rtpTimestamp=${rtpTimestamp} rtpMatched=${Boolean(matched)} fallback=${Boolean(fallback)} visible=${JSON.stringify(visible)}`));
                    return;
                }
                requestAnimationFrame(sample);
            };

            sample();
        });

        function readVisibleProbeState(canvas: HTMLCanvasElement, ctx: CanvasRenderingContext2D, expectedMarkerCode: number) {
            const cx = Math.floor(canvas.width / 2);
            const cy = Math.floor(canvas.height / 2);
            const radius = 20;
            const center = ctx.getImageData(cx - radius, cy - radius, radius * 2, radius * 2).data;
            let brightnessTotal = 0;
            let brightnessCount = 0;
            for (let i = 0; i < center.length; i += 4) {
                brightnessTotal += (center[i] + center[i + 1] + center[i + 2]) / 3;
                brightnessCount++;
            }
            const brightness = brightnessCount > 0 ? brightnessTotal / brightnessCount : -1;
            const markerCode = readMarkerCode(ctx);
            return {
                brightness,
                markerCode,
                colorMatches: brightness >= 200,
                markerMatches: markerCode === expectedMarkerCode,
            };
        }

        function readMarkerCode(ctx: CanvasRenderingContext2D): number {
            const markerStartX = 120;
            const markerStartY = 40;
            const markerCellSize = 20;
            const markerCellGap = 10;
            let code = 0;
            for (let bit = 0; bit < 16; bit++) {
                const x = markerStartX + bit * (markerCellSize + markerCellGap);
                const brightness = averageCellBrightness(ctx, x, markerStartY, markerCellSize);
                if (brightness < 120) code++;
                else break;
            }
            return code;
        }

        function averageCellBrightness(ctx: CanvasRenderingContext2D, x: number, y: number, size: number): number {
            const data = ctx.getImageData(x, y, size, size).data;
            let total = 0;
            let count = 0;
            for (let i = 0; i < data.length; i += 4) {
                total += (data[i] + data[i + 1] + data[i + 2]) / 3;
                count++;
            }
            return count > 0 ? total / count : 255;
        }
    }, {
        marker: trace.marker,
        rtpTimestamp: trace.firstPacketTimestamp,
        earliestNow: earliestInputNowMs,
        minFrames: minPresentedFrames,
        allowFallback: allowVisualFallback,
    });
}

async function readBrowserModeState(page: Page): Promise<BrowserModeState> {
    return await page.evaluate(() => {
        const checkbox = document.getElementById('webrtc-low-latency-checkbox') as HTMLInputElement | null;
        const receivers = window.rtcPeer?.getReceivers?.() ?? [];
        return {
            lowLatencyMode: Boolean(window.webrtcManager?.lowLatencyMode),
            checkboxChecked: checkbox ? checkbox.checked : null,
            receiverHints: receivers.map(receiver => {
                const lowLatencyReceiver = receiver as RTCRtpReceiver & {
                    playoutDelayHint?: number | null;
                    jitterBufferTarget?: number | null;
                };
                return {
                    playoutDelayHintSupported: 'playoutDelayHint' in lowLatencyReceiver,
                    playoutDelayHint: lowLatencyReceiver.playoutDelayHint ?? null,
                    jitterBufferTargetSupported: 'jitterBufferTarget' in lowLatencyReceiver,
                    jitterBufferTarget: lowLatencyReceiver.jitterBufferTarget ?? null,
                };
            }),
        };
    });
}

function epochMsToBrowserNow(value: number | null, timeOriginMs: number): number | null {
    return typeof value === 'number' && Number.isFinite(value) ? value - timeOriginMs : null;
}

function buildStageBreakdown(
    inputSentAtMs: number,
    serverTrace: ServerLatencyTrace,
    frame: FrameMetadataSample,
    clockSync: ClockSync,
    timeOriginMs: number,
): StageBreakdown {
    const remoteRequestAtBrowserNow = serverTrace.requestedAtMs + clockSync.offsetMs;
    const remoteDrawAtBrowserNow = serverTrace.drawnAtMs + clockSync.offsetMs;
    const rtpTimestampMatched = (frame as PresentedFrameSample).rtpTimestampMatches === true;
    const firstFrameBroadcastAtBrowserNow = serverTrace.firstFrameBroadcastAtMs > 0
        ? serverTrace.firstFrameBroadcastAtMs + clockSync.offsetMs
        : null;
    const callbackAtBrowserNow = epochMsToBrowserNow(frame.callbackAtMs, timeOriginMs)!;
    const receiveAtBrowserNow = epochMsToBrowserNow(frame.receiveAtMs, timeOriginMs);
    const presentationAtBrowserNow = epochMsToBrowserNow(frame.presentationAtMs, timeOriginMs);
    const expectedDisplayAtBrowserNow = epochMsToBrowserNow(frame.expectedDisplayAtMs, timeOriginMs);
    const decodeReadyAtBrowserNow = receiveAtBrowserNow !== null && frame.processingDurationMs !== null
        ? receiveAtBrowserNow + frame.processingDurationMs
        : null;

    return {
        inputToRemoteRequestMs: remoteRequestAtBrowserNow - inputSentAtMs,
        remoteRequestToDrawMs: remoteDrawAtBrowserNow - remoteRequestAtBrowserNow,
        remoteDrawToFirstFrameBroadcastMs: firstFrameBroadcastAtBrowserNow !== null ? firstFrameBroadcastAtBrowserNow - remoteDrawAtBrowserNow : null,
        firstFrameBroadcastToReceiveMs: rtpTimestampMatched && firstFrameBroadcastAtBrowserNow !== null && receiveAtBrowserNow !== null ? receiveAtBrowserNow - firstFrameBroadcastAtBrowserNow : null,
        receiveToDecodeReadyMs: frame.processingDurationMs,
        decodeReadyToComposeMs: decodeReadyAtBrowserNow !== null && presentationAtBrowserNow !== null ? presentationAtBrowserNow - decodeReadyAtBrowserNow : null,
        composeToExpectedDisplayMs: presentationAtBrowserNow !== null && expectedDisplayAtBrowserNow !== null ? expectedDisplayAtBrowserNow - presentationAtBrowserNow : null,
        expectedDisplayToCallbackMs: expectedDisplayAtBrowserNow !== null ? callbackAtBrowserNow - expectedDisplayAtBrowserNow : null,
        remoteDrawToBrowserCallbackMs: callbackAtBrowserNow - remoteDrawAtBrowserNow,
        inputToBrowserCallbackMs: callbackAtBrowserNow - inputSentAtMs,
    };
}

function percentile(sortedValues: number[], p: number): number | null {
    if (sortedValues.length === 0) return null;
    const index = Math.min(sortedValues.length - 1, Math.max(0, Math.round((sortedValues.length - 1) * p)));
    return sortedValues[index];
}

function summarize(values: Array<number | null>): StageStats {
    const usable = values.filter((value): value is number => typeof value === 'number' && Number.isFinite(value)).sort((a, b) => a - b);
    if (usable.length === 0) {
        return { count: 0, min: null, p10: null, median: null, mean: null, p90: null, max: null };
    }
    const mean = usable.reduce((sum, value) => sum + value, 0) / usable.length;
    return {
        count: usable.length,
        min: usable[0],
        p10: percentile(usable, 0.10),
        median: percentile(usable, 0.50),
        mean,
        p90: percentile(usable, 0.90),
        max: usable[usable.length - 1],
    };
}

function buildStageStats(trials: BreakdownTrial[]): Record<keyof StageBreakdown, StageStats> {
    const stageNames: Array<keyof StageBreakdown> = [
        'inputToRemoteRequestMs',
        'remoteRequestToDrawMs',
        'remoteDrawToFirstFrameBroadcastMs',
        'firstFrameBroadcastToReceiveMs',
        'receiveToDecodeReadyMs',
        'decodeReadyToComposeMs',
        'composeToExpectedDisplayMs',
        'expectedDisplayToCallbackMs',
        'remoteDrawToBrowserCallbackMs',
        'inputToBrowserCallbackMs',
    ];
    return Object.fromEntries(stageNames.map(stage => [
        stage,
        summarize(trials.map(trial => trial.stagesMs[stage])),
    ])) as Record<keyof StageBreakdown, StageStats>;
}

async function collectModeSummary(
    page: Page,
    mode: LatencyMode,
    baseUrl: string,
    containerName: string,
): Promise<BreakdownSummary> {
    console.log(`[${mode}] verifying container mode`);
    const containerLowLatency = run(`docker exec ${containerName} printenv WEBRTC_LOW_LATENCY || true`);
    expect(containerLowLatency === 'true', `${mode} container WEBRTC_LOW_LATENCY`).toBe(mode === 'ull');

    console.log(`[${mode}] opening viewer`);
    await page.goto(baseUrl);
    await page.click('body');
    await page.setViewportSize({ width: TARGET_VIEWPORT_WIDTH, height: TARGET_VIEWPORT_HEIGHT });
    console.log(`[${mode}] waiting for WebRTC status`);
    await expect(page.locator('#status')).toContainText(/\[WebRTC/, { timeout: 45000 });
    console.log(`[${mode}] waiting for decoded WebRTC frames`);
    await waitForDecodedFrames(page, `${mode} stream`);
    await initPresentedFrameTracker(page);

    console.log(`[${mode}] syncing browser/server clocks`);
    const clockSync = await syncBrowserToServerClock(page, baseUrl);
    const timeOriginMs = await page.evaluate(() => performance.timeOrigin);
    console.log(`[${mode}] verifying effective browser low-latency state`);
    const browserMode = await readBrowserModeState(page);
    expect(browserMode.lowLatencyMode, `${mode} browser lowLatencyMode`).toBe(mode === 'ull');
    expect(browserMode.checkboxChecked, `${mode} low-latency checkbox`).toBe(mode === 'ull');
    if (mode === 'ull') {
        for (const hint of browserMode.receiverHints) {
            if (hint.playoutDelayHintSupported) expect(hint.playoutDelayHint).toBe(0);
            if (hint.jitterBufferTargetSupported) expect(hint.jitterBufferTarget).toBe(0);
        }
    }

    console.log(`[${mode}] launching remote latency probe`);
    await launchProbe(containerName);

    for (let i = 0; i < WARMUP_TRIALS; i++) {
        let state = readProbeState(containerName);
        await moveProbeOutside(page, containerName);
        const cursor = await readPresentedFrameCursor(page);
        state = readProbeState(containerName);
        await waitForVisibleProbeState(page, 'black', state.marker, cursor.presentedFrames);
        const trigger = await triggerProbe(page, containerName, state.marker);
        await waitForServerLatencyTrace(baseUrl, trigger.state.marker);
        await waitForVisibleProbeState(page, 'white', trigger.state.marker, cursor.presentedFrames);
    }

    const trials: BreakdownTrial[] = [];
    const maxMeasuredAttempts = MEASURED_TRIALS + Math.max(10, Math.ceil(MEASURED_TRIALS * 0.5));
    for (let attempt = 1; trials.length < MEASURED_TRIALS && attempt <= maxMeasuredAttempts; attempt++) {
        console.log(`[${mode}] measured trial ${trials.length + 1}/${MEASURED_TRIALS} (attempt ${attempt}/${maxMeasuredAttempts})`);
        let state = readProbeState(containerName);
        await moveProbeOutside(page, containerName);
        const settleCursor = await readPresentedFrameCursor(page);
        state = readProbeState(containerName);
        await waitForVisibleProbeState(page, 'black', state.marker, settleCursor.presentedFrames);

        const cursor = await readPresentedFrameCursor(page);
        const trigger = await triggerProbe(page, containerName, state.marker);
        state = trigger.state;
        const serverTrace = await waitForServerLatencyTrace(baseUrl, state.marker);
        let matchedFrame: PresentedFrameSample;
        try {
            matchedFrame = await waitForPresentedFrameMatch(page, serverTrace, trigger.inputSentAtMs, cursor.presentedFrames, false);
        } catch (error) {
            console.log(`[${mode}] skipping marker ${state.marker}: ${error instanceof Error ? error.message : String(error)}`);
            continue;
        }
        const stagesMs = buildStageBreakdown(trigger.inputSentAtMs, serverTrace, matchedFrame, clockSync, timeOriginMs);

        trials.push({
            trial: trials.length + 1,
            marker: state.marker,
            color: state.color,
            inputSentAtMs: trigger.inputSentAtMs,
            requestedAtMs: state.requestedAtMs,
            drawnAtMs: state.drawnAtMs,
            markerCodeSaturated: state.marker > 16,
            clockSync: { offsetMs: clockSync.offsetMs, rttMs: clockSync.rttMs },
            serverTrace,
            frame: matchedFrame,
            stagesMs,
        });
    }

    expect(trials.length, `${mode} valid timestamp-matched trials`).toBeGreaterThanOrEqual(MIN_VALID_TRIALS);

    const observed = await page.evaluate(() => {
        const video = document.getElementById('webrtc-video') as HTMLVideoElement | null;
        const status = document.getElementById('status') as HTMLDivElement | null;
        const stats = window.getStats?.();
        return {
            streamWidth: video?.videoWidth ?? 0,
            streamHeight: video?.videoHeight ?? 0,
            statusText: status?.textContent ?? '',
            totalDecoded: stats?.totalDecoded ?? 0,
            jitterBufferDelay: typeof stats?.jitterBufferDelay === 'number' ? stats.jitterBufferDelay : null,
            jitterBufferTarget: typeof stats?.jitterBufferTarget === 'number' ? stats.jitterBufferTarget : null,
        };
    });

    expect(observed.streamWidth, `${mode} stream width`).toBeGreaterThan(0);
    expect(observed.streamHeight, `${mode} stream height`).toBeGreaterThan(0);

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
            trials: MEASURED_TRIALS,
            warmupTrials: WARMUP_TRIALS,
            minValidTrials: MIN_VALID_TRIALS,
        },
        observed: {
            ...observed,
            browserMode,
            clockSync,
        },
        trials,
        stageStats: buildStageStats(trials),
    };
}

test.describe('Browser WebRTC ULL Latency', () => {
    test.describe.configure({ retries: 0 });

    test('measures standard and ULL browser latency with timestamp-matched visible frames', async ({ browser }, testInfo) => {
        test.setTimeout(600000);

        const modes: LatencyMode[] = ['standard', 'ull'];
        const basePort = 8740 + Math.floor(Math.random() * 300);
        const summaries = new Map<LatencyMode, BreakdownSummary>();
        const containers = new Map<LatencyMode, { name: string; port: number }>();

        try {
            for (const [index, mode] of modes.entries()) {
                const port = basePort + index;
                const containerName = `llrdc-webrtc-ull-test-${mode}`;
                containers.set(mode, { name: containerName, port });

                const url = await startContainer(mode, port, containerName);
                const page = await browser.newPage();
                page.on('console', msg => console.log(`[${mode}] browser console ${msg.type()}: ${msg.text()}`));
                page.on('pageerror', error => console.log(`[${mode}] browser pageerror: ${error.message}`));
                try {
                    const summary = await collectModeSummary(page, mode, url, containerName);
                    summaries.set(mode, summary);
                } finally {
                    await page.close();
                    await stopContainer(containerName, port);
                }
            }
        } finally {
            for (const { name, port } of containers.values()) {
                await stopContainer(name, port);
            }
        }

        const standardSummary = summaries.get('standard');
        const ullSummary = summaries.get('ull');
        expect(standardSummary).toBeDefined();
        expect(ullSummary).toBeDefined();

        const deltaMedian = Object.fromEntries(Object.keys(standardSummary!.stageStats).map(stageName => {
            const stage = stageName as keyof StageBreakdown;
            const standardMedian = standardSummary!.stageStats[stage].median;
            const ullMedian = ullSummary!.stageStats[stage].median;
            return [stage, typeof standardMedian === 'number' && typeof ullMedian === 'number' ? ullMedian - standardMedian : null];
        })) as Partial<Record<keyof StageBreakdown, number | null>>;

        if (ASSERT_ULL_IMPROVES) {
            const transitDelta = deltaMedian.firstFrameBroadcastToReceiveMs;
            expect(transitDelta, 'ULL median firstFrameBroadcastToReceiveMs delta').not.toBeNull();
            expect(transitDelta!).toBeLessThanOrEqual(-ASSERT_ULL_IMPROVEMENT_MS);
        }

        const result: BenchmarkResult = {
            capturedAt: new Date().toISOString(),
            modes: [standardSummary!, ullSummary!],
            deltaMedian,
        };

        const artifactDir = join(process.cwd(), 'test-results', 'browser-ull-latency');
        mkdirSync(artifactDir, { recursive: true });
        writeFileSync(join(artifactDir, 'webrtc-ull-comparison.json'), JSON.stringify(result, null, 2));

        console.log('Browser WebRTC ULL latency summary:');
        console.log(JSON.stringify(result, null, 2));

        await testInfo.attach('webrtc-ull-comparison', {
            body: Buffer.from(JSON.stringify(result, null, 2)),
            contentType: 'application/json',
        });
    });
});
