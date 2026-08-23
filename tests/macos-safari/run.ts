import { execFileSync } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { BrowserAdapter, BrowserName, createBrowserAdapter } from './browser-adapter';

const BASE_URL = process.env.MACOS_TEST_BASE_URL ?? 'http://127.0.0.1:8080';
const CONTAINER_NAME = process.env.MACOS_TEST_CONTAINER ?? 'llrdc-macos';
const ARTIFACT_DIR = process.env.MACOS_TEST_ARTIFACT_DIR ?? '.artefact/macos-browser';

type State = {
    webtransportActive: boolean;
    wsConnected: boolean;
    videoCodec: string;
    totalDecoded: number;
    lastPresentedFrame?: unknown;
};

type Stats = { fps: number; totalDecoded: number; bytesReceived?: number; webtransportFps?: number };

function sleep(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
}

function docker(args: string[], allowFailure = false): string {
    try {
        return execFileSync('docker', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
    } catch (error) {
        if (allowFailure) return '';
        throw new Error(`docker ${args.join(' ')} failed: ${String(error)}`);
    }
}

function hostCommand(command: string, args: string[], input?: string): string {
    return execFileSync(command, args, { input, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
}

function containerExec(args: string[], allowFailure = false): string {
    return docker(['exec', ...args], allowFailure);
}

async function waitForReadyz(timeoutMs = 30000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    let last = 'unreachable';
    while (Date.now() < deadline) {
        try {
            const response = await fetch(`${BASE_URL}/readyz`);
            const body = await response.json() as { ready?: boolean };
            if (response.ok && body.ready) return;
            last = JSON.stringify(body);
        } catch (error) {
            last = String(error);
        }
        await sleep(250);
    }
    throw new Error(`macOS server did not become ready: ${last}`);
}

async function state(browser: BrowserAdapter): Promise<State> {
    return await browser.evaluate<State>('() => window.__llrdcClient?.getState?.() ?? ({})');
}

async function stats(browser: BrowserAdapter): Promise<Stats> {
    return await browser.evaluate<Stats>('() => window.getStats?.() ?? ({ fps: 0, totalDecoded: 0 })');
}

async function assertStreaming(browser: BrowserAdapter): Promise<void> {
    const expectedTransport = browser.name === 'chrome' ? 'webtransport' : 'websocket';
    const condition = expectedTransport === 'webtransport'
        ? 'document.getElementById("display") && window.__llrdcClient?.getState?.().webtransportActive === true'
        : 'document.getElementById("display") && window.__llrdcClient?.getState?.().webtransportActive !== true && window.__llrdcClient?.getState?.().wsConnected === true';
    await browser.waitFor(`() => ${condition}`, 30000);
    const connected = await state(browser);
    if (expectedTransport === 'webtransport' && connected.wsConnected) {
        throw new Error('WebSocket fallback is active; Chrome requires WebTransport');
    }
    if (expectedTransport === 'websocket' && (!connected.wsConnected || connected.webtransportActive)) {
        throw new Error('Safari must use WebSocket for the current macOS Safari lane');
    }
    const before = await stats(browser);
    await sleep(2500);
    const after = await stats(browser);
    if (after.totalDecoded <= before.totalDecoded || after.fps <= 0) {
        throw new Error(`Streaming did not advance: before=${JSON.stringify(before)} after=${JSON.stringify(after)}`);
    }
}

async function waitForLog(fragment: string, timeoutMs = 20000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        if (docker(['logs', CONTAINER_NAME], true).includes(fragment)) return;
        await sleep(250);
    }
    throw new Error(`Timed out waiting for container log: ${fragment}`);
}

async function openConfig(browser: BrowserAdapter, tab?: 'stream' | 'quality' | 'performance' | 'input'): Promise<void> {
    await browser.click('#config-btn');
    if (tab) await browser.click(`.config-tab-btn[data-tab="tab-${tab}"]`);
}

async function closeConfig(browser: BrowserAdapter): Promise<void> {
    await browser.click('#config-btn');
}

async function baseline(browser: BrowserAdapter): Promise<void> {
    await waitForReadyz();
    await browser.goto(`${BASE_URL}/viewer.html`);
    await assertStreaming(browser);
    await openConfig(browser, 'stream');
    await browser.select('#video-codec-select', 'h264');
    await browser.select('#framerate-select', '30');
    await browser.select('#hdpi-select', '100');
    await browser.select('#max-res-select', '0');
    await closeConfig(browser);
    await sleep(1000);
}

async function connection(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    const current = await state(browser);
    if (current.videoCodec !== 'h264') throw new Error(`Expected H.264 baseline, got ${current.videoCodec}`);
    await assertStreaming(browser);
}

async function reconfiguration(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    await openConfig(browser, 'quality');
    await browser.select('#bandwidth-select', '1');
    await closeConfig(browser);
    await assertStreaming(browser);
    await openConfig(browser, 'stream');
    for (const fps of ['15', '30', '60']) {
        await browser.select('#framerate-select', fps);
        await closeConfig(browser);
        await waitForLog(`Agent received FPS config: ${fps}`, 30000);
        await assertStreaming(browser);
        if (fps !== '60') await openConfig(browser, 'stream');
    }
}

async function resolution(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    await openConfig(browser, 'stream');
    await browser.select('#max-res-select', '720');
    await closeConfig(browser);
    await browser.waitFor('() => document.getElementById("display")?.width === 1280 && document.getElementById("display")?.height === 720', 30000);
    await openConfig(browser, 'stream');
    await browser.select('#max-res-select', '1080');
    await closeConfig(browser);
    await browser.waitFor('() => { const c = document.getElementById("display"); return !!c && c.width === 1920 && c.height >= 1072 && c.height <= 1080 && c.height % 8 === 0; }', 30000);
    await openConfig(browser, 'stream');
    await browser.select('#max-res-select', '0');
    await closeConfig(browser);
    const first = await browser.windowSize();
    await browser.setWindowSize(1000, 700);
    await sleep(1500);
    const second = await browser.windowSize();
    if (second.width === first.width && second.height === first.height) throw new Error('Browser window did not resize');
    await browser.waitFor('() => { const c = document.getElementById("display"); return !!c && c.width > 0 && c.height > 0 && c.width % 8 === 0 && c.height % 8 === 0; }', 30000);
    await browser.setWindowSize(1324, 931);
    await assertStreaming(browser);
}

async function hdpi(browser: BrowserAdapter): Promise<void> {
    // Safari reports a device pixel ratio of 2. Keep the headed window at a
    // size that produces the capture agent's supported 1920x1072 surface;
    // larger outer windows request oversized buffers which fail when the
    // compositor switches to a 2x scale.
    await browser.setWindowSize(964, 643);
    await baseline(browser);
    await openConfig(browser, 'stream');
    await browser.select('#max-res-select', '720');
    await closeConfig(browser);
    await browser.waitFor('() => document.getElementById("display")?.width === 1280 && document.getElementById("display")?.height === 720', 30000);
    await openConfig(browser, 'stream');
    await browser.select('#hdpi-select', '200');
    await closeConfig(browser);
    await waitForLog('Agent received HDPI config: 200%', 30000);
    await waitForLog('with scale 2.000000', 30000);
    const scale = containerExec(['-u', 'remote', CONTAINER_NAME, 'bash', '-lc', 'export WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/tmp/llrdc-run; wlr-randr --output HEADLESS-1']);
    if (!scale.includes('Scale: 2.000000')) throw new Error(`Unexpected compositor scale: ${scale}`);
    await sleep(2000);
    await assertStreaming(browser);
}

async function input(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    const testFile = '/tmp/kb_safari_browser.txt';
    containerExec(['-u', 'remote', CONTAINER_NAME, 'rm', '-f', testFile], true);
    containerExec(['-u', 'remote', CONTAINER_NAME, 'rm', '-f', '/tmp/llrdc-latency-probe.json'], true);
    containerExec(['-u', 'remote', '-d', CONTAINER_NAME, 'bash', '-lc', 'export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1']);
    await sleep(500);
    const initialProbe = JSON.parse(containerExec([CONTAINER_NAME, 'cat', '/tmp/llrdc-latency-probe.json'])) as { marker: number; mouseX: number };
    // Give Safari's WebDriver input queue time to deliver motion before the
    // sampled button-down. Multiple points also avoid relying on a particular
    // outer-window size (Safari controls that window rather than a fixed
    // Playwright viewport).
    for (const [x, y] of [[10, 10], [200, 40], [100, 25]]) {
        await browser.pointerMove('#input-overlay', x, y);
        await sleep(300);
    }
    await browser.pointerClick('#input-overlay', 100, 25);
    const markerDeadline = Date.now() + 10000;
    let finalProbe = initialProbe;
    while (Date.now() < markerDeadline) {
        finalProbe = JSON.parse(containerExec([CONTAINER_NAME, 'cat', '/tmp/llrdc-latency-probe.json'])) as { marker: number; mouseX: number };
        if (finalProbe.marker > initialProbe.marker) break;
        await sleep(200);
    }
    if (finalProbe.marker <= initialProbe.marker) throw new Error(`Pointer input did not advance the latency probe: before=${JSON.stringify(initialProbe)} after=${JSON.stringify(finalProbe)}`);

    // The probe is fullscreen and intentionally owns the remote keyboard while
    // measuring pointer delivery. Close it before launching Mousepad so the
    // subsequent keyboard assertion targets the editor, not the probe.
    containerExec(['-u', 'remote', CONTAINER_NAME, 'pkill', '-x', 'latency_probe'], true);
    containerExec(['-u', 'remote', CONTAINER_NAME, 'pkill', '-x', 'mousepad'], true);
    containerExec(['-u', 'remote', '-d', CONTAINER_NAME, 'bash', '-lc', `export WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/tmp/llrdc-run GDK_BACKEND=wayland; mousepad ${testFile}`]);
    await sleep(2500);
    containerExec(['-u', 'remote', CONTAINER_NAME, 'bash', '-lc', 'export WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/tmp/llrdc-run; wlrctl toplevel maximize app_id:org.xfce.mousepad; wlrctl toplevel focus app_id:org.xfce.mousepad'], true);
    await browser.pointerClick('#display-container');
    await browser.typeText('hello');
    await browser.keyCombo('Control', 's');
    await browser.keyCombo('Control', 'q');
    await sleep(1500);
    const content = containerExec(['-u', 'remote', CONTAINER_NAME, 'cat', testFile]);
    if (content !== 'hello') throw new Error(`Keyboard input was not delivered to Mousepad: ${content}`);
    await assertStreaming(browser);
}

async function clipboard(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    const toRemote = `HOST_TO_REMOTE_${Date.now()}`;
    hostCommand('pbcopy', [], toRemote);
    await browser.pointerClick('#display-container');
    await browser.keyCombo('Meta', 'v');
    const deadline = Date.now() + 10000;
    while (Date.now() < deadline && containerExec(['-u', 'remote', '-e', 'WAYLAND_DISPLAY=wayland-0', '-e', 'XDG_RUNTIME_DIR=/tmp/llrdc-run', CONTAINER_NAME, 'wl-paste'], true) !== toRemote) await sleep(250);
    if (containerExec(['-u', 'remote', '-e', 'WAYLAND_DISPLAY=wayland-0', '-e', 'XDG_RUNTIME_DIR=/tmp/llrdc-run', CONTAINER_NAME, 'wl-paste'], true) !== toRemote) throw new Error('Host-to-remote clipboard did not arrive');
    const toHost = `REMOTE_TO_HOST_${Date.now()}`;
    containerExec(['-u', 'remote', CONTAINER_NAME, 'bash', '-lc', `printf %s ${JSON.stringify(toHost)} | WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/tmp/llrdc-run wl-copy`]);
    await browser.pointerClick('#display-container');
    await sleep(1500);
    if (hostCommand('pbpaste', []) !== toHost) throw new Error('Remote-to-host clipboard did not arrive');
}

async function codecs(browser: BrowserAdapter): Promise<void> {
    await baseline(browser);
    const codecsToTest = browser.name === 'safari' ? ['h265-444'] : ['h264-444', 'h265-444'];
    for (const codec of codecsToTest) {
        await openConfig(browser, 'stream');
        await browser.select('#video-codec-select', codec);
        await closeConfig(browser);
        await browser.waitFor(`() => window.__llrdcClient?.getState?.().videoCodec === ${JSON.stringify(codec)}`, 30000);
        await assertStreaming(browser);
    }
}

const scenarios: Record<string, (browser: BrowserAdapter) => Promise<void>> = {
    connection,
    reconfiguration,
    resolution,
    hdpi,
    input,
    clipboard,
    codecs,
};

function parseArgs(): { browser: BrowserName; scenario: string } {
    const browser = process.argv[process.argv.indexOf('--browser') + 1] as BrowserName;
    const scenario = process.argv[process.argv.indexOf('--scenario') + 1];
    if (!['chrome', 'safari'].includes(browser) || !scenario || !scenarios[scenario]) {
        throw new Error(`Usage: run.ts --browser chrome|safari --scenario ${Object.keys(scenarios).join('|')}`);
    }
    return { browser, scenario };
}

async function main(): Promise<void> {
    const { browser: browserName, scenario } = parseArgs();
    const browser = createBrowserAdapter(browserName);
    await mkdir(ARTIFACT_DIR, { recursive: true });
    try {
        await browser.start();
        await scenarios[scenario](browser);
        const successDiagnostics = await browser.evaluate('() => ({ url: location.href, state: window.__llrdcClient?.getState?.(), stats: window.getStats?.(), status: document.getElementById("status")?.textContent })');
        await writeFile(`${ARTIFACT_DIR}/${browserName}-${scenario}.json`, JSON.stringify(successDiagnostics, null, 2));
        console.log(`PASS ${browserName}/${scenario}`);
    } catch (error) {
        const artifact = `${ARTIFACT_DIR}/${browserName}-${scenario}-failure.png`;
        await browser.screenshot(artifact).catch(() => undefined);
        const diagnostics = await browser.evaluate('() => ({ url: location.href, state: window.__llrdcClient?.getState?.(), stats: window.getStats?.(), status: document.getElementById("status")?.textContent })').catch(() => ({ error: 'browser diagnostics unavailable' }));
        await writeFile(`${ARTIFACT_DIR}/${browserName}-${scenario}-failure.json`, JSON.stringify({ error: String(error), diagnostics }, null, 2));
        throw error;
    } finally {
        await browser.close();
    }
}

await main();
