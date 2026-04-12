import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from '../helpers';

type CaptureMode = 'compat' | 'direct';

interface ProbeState {
    marker: number;
    color: 'black' | 'white';
    requestedAtMs: number;
    drawnAtMs: number;
    pid: number;
}

interface TrialResult {
    trial: number;
    color: 'black' | 'white';
    inputSentAtMs: number;
    detectedAtMs: number;
    requestedAtMs: number;
    drawnAtMs: number;
    inputToPhotonMs: number;
    requestToPhotonMs: number;
    drawToPhotonMs: number;
}

interface ModeSummary {
    mode: CaptureMode;
    baseUrl: string;
    containerName: string;
    trials: TrialResult[];
    avgInputToPhotonMs: number;
    medianInputToPhotonMs: number;
    p95InputToPhotonMs: number;
    avgDrawToPhotonMs: number;
    medianDrawToPhotonMs: number;
    p95DrawToPhotonMs: number;
}

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

async function startContainer(mode: CaptureMode, port: number, containerName: string): Promise<string> {
    killPort(port);
    try {
        execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });
    } catch (e) {}

    execSync(`./docker-run.sh --nvidia --capture-mode ${mode} --detach --name ${containerName} --host-net`, {
        env: {
            ...process.env,
            PORT: port.toString(),
            HOST_PORT: port.toString(),
            CONTAINER_NAME: containerName,
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
    } catch (e) {}
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

async function waitForDecodedFrames(page: import('@playwright/test').Page, label: string) {
    await expect.poll(async () => {
        return await page.evaluate(() => window.getStats ? window.getStats().totalDecoded : 0);
    }, {
        timeout: 45000,
        message: `Wait for decoded frames during ${label}`,
    }).toBeGreaterThan(0);
}

async function disableVbr(page: import('@playwright/test').Page, containerName: string) {
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).toBeVisible();
    await page.locator('.config-tab-btn[data-tab="tab-quality"]').click();

    const vbrCheckbox = page.locator('#vbr-checkbox');
    if (await vbrCheckbox.isChecked()) {
        await vbrCheckbox.uncheck();
        await expect.poll(() => execSync(`docker logs ${containerName}`).toString(), {
            timeout: 20000,
            message: `Wait for VBR=false to be applied in ${containerName}`,
        }).toContain('Received VBR config: false');
    }

    const dtCheckbox = page.locator('#damage-tracking-checkbox');
    if (await dtCheckbox.isChecked()) {
        await dtCheckbox.uncheck();
        await expect.poll(() => execSync(`docker logs ${containerName}`).toString(), {
            timeout: 20000,
            message: `Wait for Damage Tracking=false to be applied in ${containerName}`,
        }).toContain('Received Damage Tracking config: false');
    }

    await waitForDecodedFrames(page, `VBR and Damage Tracking disabled in ${containerName}`);
    await page.click('#config-btn');
    await expect(page.locator('#config-dropdown')).not.toBeVisible();
}

function average(values: number[]): number {
    return values.reduce((sum, value) => sum + value, 0) / values.length;
}

function median(values: number[]): number {
    const sorted = [...values].sort((a, b) => a - b);
    const mid = Math.floor(sorted.length / 2);
    return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid];
}

function percentile(values: number[], pct: number): number {
    const sorted = [...values].sort((a, b) => a - b);
    const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil((pct / 100) * sorted.length) - 1));
    return sorted[index];
}

async function waitForProbeColor(
    page: import('@playwright/test').Page,
    expectedColor: 'black' | 'white',
): Promise<{ brightness: number; detectedAtMs: number }> {
    const deadline = Date.now() + 10000;

    while (Date.now() < deadline) {
        const result = await page.evaluate(({ expected }) => {
            const canvas = document.getElementById('display') as HTMLCanvasElement | null;
            if (!canvas) return { brightness: -1, detectedAtMs: 0, matches: false };
            const ctx = canvas.getContext('2d');
            if (!ctx || canvas.width === 0 || canvas.height === 0) {
                return { brightness: -1, detectedAtMs: 0, matches: false };
            }

            const cx = Math.floor(canvas.width / 2);
            const cy = Math.floor(canvas.height / 2);
            const radius = 6;
            const image = ctx.getImageData(cx - radius, cy - radius, radius * 2, radius * 2).data;
            let total = 0;
            let count = 0;
            for (let i = 0; i < image.length; i += 4) {
                total += (image[i] + image[i + 1] + image[i + 2]) / 3;
                count++;
            }
            const brightness = count > 0 ? total / count : -1;
            const matches = expected === 'white' ? brightness >= 200 : brightness <= 55;
            return {
                brightness,
                detectedAtMs: performance.timeOrigin + performance.now(),
                matches,
            };
        }, { expected: expectedColor });

        if (result.matches) {
            return {
                brightness: result.brightness,
                detectedAtMs: result.detectedAtMs,
            };
        }

        await page.waitForTimeout(16);
    }

    throw new Error(`Timed out waiting for center pixel to turn ${expectedColor}`);
}

async function clickUntilProbeToggles(
    overlay: import('@playwright/test').Locator,
    containerName: string,
    previousMarker: number,
): Promise<{ state: ProbeState; inputSentAtMs: number }> {
    let lastState = readProbeState(containerName);

    for (let attempt = 1; attempt <= 3; attempt++) {
        const inputSentAtMs = Date.now();
        await overlay.click();

        try {
            await expect.poll(() => readProbeState(containerName), {
                timeout: 1500,
                message: `Wait for probe marker ${previousMarker + 1} in ${containerName} (attempt ${attempt})`,
            }).toMatchObject({
                marker: previousMarker + 1,
            });

            return {
                state: readProbeState(containerName),
                inputSentAtMs,
            };
        } catch (_error) {
            lastState = readProbeState(containerName);
            await overlay.hover();
            await overlay.page().waitForTimeout(100);
        }
    }

    throw new Error(`Probe marker did not advance in ${containerName}; last marker=${lastState.marker}`);
}

async function launchProbe(containerName: string) {
    run(`docker exec -u remote -d ${containerName} bash -lc "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1"`);
    await waitForProbeState(containerName);
}

async function collectModeSummary(
    page: import('@playwright/test').Page,
    mode: CaptureMode,
    baseUrl: string,
    containerName: string,
): Promise<ModeSummary> {
    const ready = await fetchReadyz(baseUrl);
    if (mode === 'direct') {
        expect(ready.directBuffer?.active).toBe(true);
    } else {
        expect(ready.directBuffer?.active).not.toBe(true);
    }

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(baseUrl);
    await page.click('body');
    await expect(page.locator('#status')).toContainText(/\[.*\]/, { timeout: 45000 });
    await waitForDecodedFrames(page, `${mode} initial stream`);
    await disableVbr(page, containerName);
    await launchProbe(containerName);

    await expect.poll(async () => readProbeState(containerName).color, {
        timeout: 10000,
        message: `Wait for probe to report black in ${containerName}`,
    }).toBe('black');
    await waitForProbeColor(page, 'black');

    const overlay = page.locator('#input-overlay');
    await expect(overlay).toBeVisible();
    await overlay.hover();

    // Warm up both the input path and the displayed-frame detection before collecting samples.
    let state = readProbeState(containerName);
    for (let i = 0; i < 2; i++) {
        const toggle = await clickUntilProbeToggles(overlay, containerName, state.marker);
        state = toggle.state;
        await waitForProbeColor(page, state.color);
        await page.waitForTimeout(150);
    }

    const trials: TrialResult[] = [];

    for (let trial = 1; trial <= 10; trial++) {
        const toggle = await clickUntilProbeToggles(overlay, containerName, state.marker);
        const inputSentAtMs = toggle.inputSentAtMs;
        state = toggle.state;
        const detection = await waitForProbeColor(page, state.color);

        trials.push({
            trial,
            color: state.color,
            inputSentAtMs,
            detectedAtMs: detection.detectedAtMs,
            requestedAtMs: state.requestedAtMs,
            drawnAtMs: state.drawnAtMs,
            inputToPhotonMs: detection.detectedAtMs - inputSentAtMs,
            requestToPhotonMs: detection.detectedAtMs - state.requestedAtMs,
            drawToPhotonMs: detection.detectedAtMs - state.drawnAtMs,
        });

        await page.waitForTimeout(300);
    }

    const inputToPhotonValues = trials.map(trial => trial.inputToPhotonMs);
    const drawToPhotonValues = trials.map(trial => trial.drawToPhotonMs);

    return {
        mode,
        baseUrl,
        containerName,
        trials,
        avgInputToPhotonMs: average(inputToPhotonValues),
        medianInputToPhotonMs: median(inputToPhotonValues),
        p95InputToPhotonMs: percentile(inputToPhotonValues, 95),
        avgDrawToPhotonMs: average(drawToPhotonValues),
        medianDrawToPhotonMs: median(drawToPhotonValues),
        p95DrawToPhotonMs: percentile(drawToPhotonValues, 95),
    };
}

test.describe('Wayland Latency Benchmark', () => {
    test('should measure repeatable event-to-photon latency for compat vs direct', async ({ browser }, testInfo) => {
        test.setTimeout(360000);

        const compatPort = 8611;
        const directPort = 8612;
        const compatContainer = 'llrdc-latency-compat';
        const directContainer = 'llrdc-latency-direct';

        let compatSummary: ModeSummary | null = null;
        let directSummary: ModeSummary | null = null;

        try {
            const compatUrl = await startContainer('compat', compatPort, compatContainer);
            const compatPage = await browser.newPage();
            compatSummary = await collectModeSummary(compatPage, 'compat', compatUrl, compatContainer);
            await compatPage.close();

            const directUrl = await startContainer('direct', directPort, directContainer);
            const directPage = await browser.newPage();
            directSummary = await collectModeSummary(directPage, 'direct', directUrl, directContainer);
            await directPage.close();
        } finally {
            await stopContainer(compatContainer, compatPort);
            await stopContainer(directContainer, directPort);
        }

        expect(compatSummary).not.toBeNull();
        expect(directSummary).not.toBeNull();

        const result = {
            capturedAt: new Date().toISOString(),
            compat: compatSummary,
            direct: directSummary,
            delta: {
                avgInputToPhotonMs: (directSummary?.avgInputToPhotonMs || 0) - (compatSummary?.avgInputToPhotonMs || 0),
                medianInputToPhotonMs: (directSummary?.medianInputToPhotonMs || 0) - (compatSummary?.medianInputToPhotonMs || 0),
                p95InputToPhotonMs: (directSummary?.p95InputToPhotonMs || 0) - (compatSummary?.p95InputToPhotonMs || 0),
                avgDrawToPhotonMs: (directSummary?.avgDrawToPhotonMs || 0) - (compatSummary?.avgDrawToPhotonMs || 0),
                medianDrawToPhotonMs: (directSummary?.medianDrawToPhotonMs || 0) - (compatSummary?.medianDrawToPhotonMs || 0),
                p95DrawToPhotonMs: (directSummary?.p95DrawToPhotonMs || 0) - (compatSummary?.p95DrawToPhotonMs || 0),
            },
        };

        console.log('Latency benchmark summary:');
        console.log(JSON.stringify(result, null, 2));

        await testInfo.attach('latency-benchmark', {
            body: Buffer.from(JSON.stringify(result, null, 2)),
            contentType: 'application/json',
        });
    });
});
