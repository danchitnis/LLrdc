import { expect, Page } from '@playwright/test';
import { fetchReadyz, readClientStats, waitForStreamingFrames } from '../helpers';

export type CaptureMode = 'compat' | 'direct';
export type BrowserTransport = 'WebTransport' | 'WebSocket';

export interface ConnectionTarget {
    serverHost: string;
    port: number;
    captureMode: CaptureMode;
    serverUrl: string;
    viewerUrl: string;
    expectedTransport: BrowserTransport;
    statusCodec: string;
    browserSurface: 'macos' | 'linux';
}

function parsePort(value: string | undefined): number {
    const port = Number(value || '8080');
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
        throw new Error(`Invalid LLRDC_SERVER_PORT: ${value}`);
    }
    return port;
}

function parseCaptureMode(value: string | undefined): CaptureMode {
    if (value === 'compat' || value === 'direct') return value;
    throw new Error('LLRDC_CAPTURE_MODE must be compat or direct');
}

export function resolveConnectionTarget(): ConnectionTarget {
    const browserSurface = process.platform === 'darwin' ? 'macos' : 'linux';
    if (process.platform !== 'darwin' && process.platform !== 'linux') {
        throw new Error(`Unsupported browser host platform: ${process.platform}`);
    }

    const port = parsePort(process.env.LLRDC_SERVER_PORT);
    // Both browser surfaces target the nzxt5 server by default. Using its
    // hostname keeps Chrome's certificate interstitial bypassable; localhost
    // is treated specially by Chrome and may reject the certificate outright.
    const serverHost = process.env.LLRDC_SERVER_HOST || 'nzxt5';
    const serverUrl = (process.env.LLRDC_SERVER_URL || `http://${serverHost}:${port}`).replace(/\/$/, '');
    const expectedTransport = (process.env.LLRDC_EXPECTED_TRANSPORT || 'WebTransport') as BrowserTransport;
    if (expectedTransport !== 'WebSocket' && expectedTransport !== 'WebTransport') {
        throw new Error(`Invalid LLRDC_EXPECTED_TRANSPORT: ${expectedTransport}`);
    }

    const viewerUrl = process.env.LLRDC_VIEWER_URL || `https://${serverHost}:${port + 10}/`;

    return {
        serverHost,
        port,
        captureMode: parseCaptureMode(process.env.LLRDC_CAPTURE_MODE),
        serverUrl,
        viewerUrl,
        expectedTransport,
        statusCodec: 'h264',
        browserSurface,
    };
}

function isCertificateNavigationError(error: unknown): boolean {
    const message = error instanceof Error ? error.message : String(error);
    return /ERR_CERT_|certificate|self-signed/i.test(message);
}

async function clickCertificateInterstitial(page: Page, viewerUrl: string): Promise<boolean> {
    await page.bringToFront();
    const detailsButton = page.locator('#details-button');
    if (!await detailsButton.isVisible({ timeout: 5000 }).catch(() => false)) return false;

    await detailsButton.click();
    const proceedLink = page.locator('#proceed-link');
    await expect(proceedLink).toBeVisible({ timeout: 5000 });
    await proceedLink.click();
    await page.waitForLoadState('domcontentloaded');
    console.log(`Accepted Chrome certificate warning for ${viewerUrl}`);
    return true;
}

/**
 * Navigate to the HTTPS/WebTransport viewer and accept Chrome's self-signed
 * certificate warning when it is shown. Chrome may remember the decision, so
 * the interstitial is deliberately optional.
 */
export async function navigateToHttpsViewer(page: Page, target: ConnectionTarget): Promise<void> {
    if (!target.viewerUrl.startsWith('https://')) {
        await page.goto(target.viewerUrl, { waitUntil: 'domcontentloaded' });
        return;
    }

    let navigationError: unknown;
    try {
        await page.goto(target.viewerUrl, { waitUntil: 'domcontentloaded' });
    } catch (error) {
        if (!isCertificateNavigationError(error)) throw error;
        navigationError = error;
    }

    let detailsVisible = await page.locator('#details-button').isVisible({ timeout: 10000 }).catch(() => false);
    for (let attempt = 0; !detailsVisible && navigationError && attempt < 2; attempt += 1) {
        // Chromium can report ERR_CERT_AUTHORITY_INVALID before committing its
        // error document. A short retry gives the interstitial a chance to
        // become the active page without weakening certificate validation.
        await page.waitForTimeout(500);
        try {
            await page.goto(target.viewerUrl, { waitUntil: 'commit' });
        } catch (error) {
            if (!isCertificateNavigationError(error)) throw error;
            navigationError = error;
        }
        detailsVisible = await page.locator('#details-button').isVisible({ timeout: 5000 }).catch(() => false);
    }

    if (detailsVisible) {
        await clickCertificateInterstitial(page, target.viewerUrl);
    } else if (navigationError) {
        // On some headed Wayland launches the first page remains on
        // chrome-error://chromewebdata without exposing the interstitial DOM.
        // A second page in the same context reliably exposes Chrome's UI and
        // shares the resulting certificate decision with the test page.
        const certificatePage = await page.context().newPage();
        try {
            await certificatePage.bringToFront();
            let certificateNavigationError: unknown;
            try {
                await certificatePage.goto(target.viewerUrl, { waitUntil: 'domcontentloaded' });
            } catch (error) {
                if (!isCertificateNavigationError(error)) throw error;
                certificateNavigationError = error;
            }
            const acceptedWarning = await clickCertificateInterstitial(certificatePage, target.viewerUrl);
            const certificateAlreadyAccepted = await certificatePage.locator('#status').isVisible({ timeout: 5000 }).catch(() => false);
            if (!acceptedWarning && !certificateAlreadyAccepted) {
                const cause = certificateNavigationError || navigationError;
                throw new Error(`HTTPS navigation failed without a certificate interstitial: ${String(cause)}`);
            }
        } finally {
            await certificatePage.close();
        }
        await page.goto(target.viewerUrl, { waitUntil: 'domcontentloaded' });
    }

    await expect(page.locator('#status')).toBeVisible({ timeout: 15000 });
}

export async function assertInstalledHeadedChrome(page: Page): Promise<void> {
    const cdp = await page.context().newCDPSession(page);
    const browserVersion = await cdp.send('Browser.getVersion');
    console.log(`Browser product: ${browserVersion.product}`);
    console.log(`Configured executable: ${process.env.PLAYWRIGHT_CHROME_EXECUTABLE || 'Chrome channel'}`);
    expect(browserVersion.product).toMatch(/^Chrome\//);
    expect(browserVersion.userAgent).not.toContain('HeadlessChrome');
}

export async function assertStreamingConnection(
    page: Page,
    target: ConnectionTarget,
    message: string,
): Promise<void> {
    const transportPattern = target.expectedTransport === 'WebTransport' ? 'WebTransport' : 'WebSocket';
    await expect(page.locator('#status')).toContainText(
        new RegExp(`\\[${transportPattern}\\s+${target.statusCodec}\\b`, 'i'),
        { timeout: 45000 },
    );

    await waitForStreamingFrames(page, message, 30000);

    const before = await readClientStats(page);
    await page.mouse.move(320, 240);
    await page.waitForTimeout(1500);
    const after = await readClientStats(page);
    expect(after.totalDecoded).toBeGreaterThan(before.totalDecoded);
    expect(after.totalDecoded).toBeGreaterThan(10);
    expect(after.fps).toBeGreaterThan(0);

    console.log(`Final status: ${await page.locator('#status').textContent()}`);
    console.log(`Decoded frames: ${after.totalDecoded}; FPS: ${after.fps}`);
}

export async function waitForConnectionReady(target: ConnectionTarget): Promise<void> {
    await expect.poll(() => fetchReadyz(target.serverUrl), {
        timeout: 30000,
        message: `Wait for ${target.serverUrl}/readyz`,
    }).toMatchObject({ ready: true });
}
