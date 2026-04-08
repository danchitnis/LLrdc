import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { fetchReadyz, waitForServerReady } from './helpers';

type CaptureMode = 'compat' | 'direct';
type ScenarioName = 'idle' | 'active';

interface Sample {
    second: number;
    fps: number;
    latencyMs: number;
    displayedFfmpegCpu: number;
    recorderCpu: number;
    containerCpu: number;
    totalDecoded: number;
    bytesReceived: number;
}

interface ScenarioSummary {
    name: ScenarioName;
    samples: Sample[];
    avgFps: number;
    avgLatencyMs: number;
    avgDisplayedFfmpegCpu: number;
    avgRecorderCpu: number;
    avgContainerCpu: number;
}

interface MeasurementSummary {
    mode: CaptureMode;
    baseUrl: string;
    containerName: string;
    vbrDisabled: boolean;
    scenarios: Record<ScenarioName, ScenarioSummary>;
}

function run(cmd: string): string {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim();
}

function killPort(port: number) {
    try {
        execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
    } catch (e) {}
}

function dockerCpuPercent(containerName: string): number {
    const raw = run(`docker stats --no-stream --format "{{.CPUPerc}}" ${containerName}`);
    return parseFloat(raw.replace('%', '').trim()) || 0;
}

function recorderCpuPercent(containerName: string): number {
    try {
        const raw = run(`docker exec ${containerName} bash -lc "ps -C wf-recorder -o %cpu= | awk '{sum += \\$1} END {print sum+0}'"`);
        return parseFloat(raw) || 0;
    } catch (e) {
        return 0;
    }
}

function average(values: number[]): number {
    if (values.length === 0) return 0;
    return values.reduce((sum, value) => sum + value, 0) / values.length;
}

function parseStatusMetric(text: string, pattern: RegExp): number {
    const match = text.match(pattern);
    return match ? parseFloat(match[1]) : 0;
}

async function startContainer(mode: CaptureMode, port: number, containerName: string): Promise<string> {
    killPort(port);
    try {
        execSync(`docker rm -f ${containerName}`, { stdio: 'ignore' });
    } catch (e) {}

    execSync(`./docker-run.sh --gpu --capture-mode ${mode} --detach --name ${containerName}`, {
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
}

async function collectScenario(
    page: import('@playwright/test').Page,
    containerName: string,
    scenario: ScenarioName,
): Promise<ScenarioSummary> {
    const overlay = page.locator('#input-overlay');
    await overlay.hover();

    const samples: Sample[] = [];

    for (let second = 1; second <= 10; second++) {
        if (scenario === 'active') {
            for (let step = 0; step < 5; step++) {
                await page.mouse.move(120 + ((second + step) % 10) * 70, 120 + ((second * 3 + step) % 8) * 55);
                await page.waitForTimeout(200);
            }
        } else {
            await page.waitForTimeout(1000);
        }

        const snapshot = await page.evaluate(() => {
            const stats = window.getStats ? window.getStats() : {
                fps: 0,
                latency: 0,
                totalDecoded: 0,
                webrtcFps: 0,
                bytesReceived: 0,
            };
            const statusText = document.getElementById('status')?.textContent || '';
            return { stats, statusText };
        });

        samples.push({
            second,
            fps: snapshot.stats.fps || 0,
            latencyMs: parseStatusMetric(snapshot.statusText, /Latency \(Video\): ([\d.]+)ms/),
            displayedFfmpegCpu: parseStatusMetric(snapshot.statusText, /FFmpeg CPU: ([\d.]+)%/),
            recorderCpu: recorderCpuPercent(containerName),
            containerCpu: dockerCpuPercent(containerName),
            totalDecoded: snapshot.stats.totalDecoded || 0,
            bytesReceived: snapshot.stats.bytesReceived || 0,
        });
    }

    return {
        name: scenario,
        samples,
        avgFps: average(samples.map(sample => sample.fps)),
        avgLatencyMs: average(samples.map(sample => sample.latencyMs)),
        avgDisplayedFfmpegCpu: average(samples.map(sample => sample.displayedFfmpegCpu)),
        avgRecorderCpu: average(samples.map(sample => sample.recorderCpu)),
        avgContainerCpu: average(samples.map(sample => sample.containerCpu)),
    };
}

async function collectSummary(
    page: import('@playwright/test').Page,
    mode: CaptureMode,
    baseUrl: string,
    containerName: string,
): Promise<MeasurementSummary> {
    const ready = await fetchReadyz(baseUrl);

    if (mode === 'direct') {
        expect(ready.directBuffer?.active).toBe(true);
    } else {
        expect(ready.directBuffer?.active).not.toBe(true);
    }

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(baseUrl);
    await page.click('body');

    const statusEl = page.locator('#status');
    await expect(statusEl).toContainText(/\[WebRTC|\[WebCodecs/, { timeout: 45000 });
    await waitForDecodedFrames(page, `${mode} initial stream`);

    await disableVbr(page, containerName);

    return {
        mode,
        baseUrl,
        containerName,
        vbrDisabled: true,
        scenarios: {
            idle: await collectScenario(page, containerName, 'idle'),
            active: await collectScenario(page, containerName, 'active'),
        },
    };
}

test.describe('Wayland Direct Buffer Benchmark', () => {
    test('should measure compat vs direct on the same host with VBR disabled', async ({ browser }, testInfo) => {
        test.setTimeout(360000);

        const compatPort = 8601;
        const directPort = 8602;
        const compatContainer = 'llrdc-bench-compat';
        const directContainer = 'llrdc-bench-direct';

        let compatSummary: MeasurementSummary | null = null;
        let directSummary: MeasurementSummary | null = null;

        try {
            const compatUrl = await startContainer('compat', compatPort, compatContainer);
            const compatPage = await browser.newPage();
            compatSummary = await collectSummary(compatPage, 'compat', compatUrl, compatContainer);
            await compatPage.close();

            const directUrl = await startContainer('direct', directPort, directContainer);
            const directPage = await browser.newPage();
            directSummary = await collectSummary(directPage, 'direct', directUrl, directContainer);
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
                idle: {
                    latencyMs: (directSummary?.scenarios.idle.avgLatencyMs || 0) - (compatSummary?.scenarios.idle.avgLatencyMs || 0),
                    displayedFfmpegCpu: (directSummary?.scenarios.idle.avgDisplayedFfmpegCpu || 0) - (compatSummary?.scenarios.idle.avgDisplayedFfmpegCpu || 0),
                    recorderCpu: (directSummary?.scenarios.idle.avgRecorderCpu || 0) - (compatSummary?.scenarios.idle.avgRecorderCpu || 0),
                    containerCpu: (directSummary?.scenarios.idle.avgContainerCpu || 0) - (compatSummary?.scenarios.idle.avgContainerCpu || 0),
                    fps: (directSummary?.scenarios.idle.avgFps || 0) - (compatSummary?.scenarios.idle.avgFps || 0),
                },
                active: {
                    latencyMs: (directSummary?.scenarios.active.avgLatencyMs || 0) - (compatSummary?.scenarios.active.avgLatencyMs || 0),
                    displayedFfmpegCpu: (directSummary?.scenarios.active.avgDisplayedFfmpegCpu || 0) - (compatSummary?.scenarios.active.avgDisplayedFfmpegCpu || 0),
                    recorderCpu: (directSummary?.scenarios.active.avgRecorderCpu || 0) - (compatSummary?.scenarios.active.avgRecorderCpu || 0),
                    containerCpu: (directSummary?.scenarios.active.avgContainerCpu || 0) - (compatSummary?.scenarios.active.avgContainerCpu || 0),
                    fps: (directSummary?.scenarios.active.avgFps || 0) - (compatSummary?.scenarios.active.avgFps || 0),
                },
            },
        };

        console.log('Direct-buffer benchmark summary (VBR disabled):');
        console.log(JSON.stringify(result, null, 2));

        await testInfo.attach('direct-buffer-benchmark-vbr-disabled', {
            body: Buffer.from(JSON.stringify(result, null, 2)),
            contentType: 'application/json',
        });
    });
});
