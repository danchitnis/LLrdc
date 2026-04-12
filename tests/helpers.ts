import { expect, Page } from '@playwright/test';

export interface ContainerImage {
    name: string;
    tag: string;
    ref: string;
}

export function getContainerImage(defaultTag = 'latest'): ContainerImage {
    const image = process.env.CONTAINER_IMAGE || `danchitnis/llrdc:${defaultTag}`;
    const lastSlash = image.lastIndexOf('/');
    const lastColon = image.lastIndexOf(':');

    if (lastColon > lastSlash) {
        const name = image.slice(0, lastColon);
        const tag = image.slice(lastColon + 1);
        return { name, tag, ref: image };
    }

    return {
        name: image,
        tag: defaultTag,
        ref: `${image}:${defaultTag}`,
    };
}

export async function waitForServerReady(baseUrl: string, timeoutMs = 45000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    let lastStatus = 'server not reachable yet';

    while (Date.now() < deadline) {
        try {
            const response = await fetch(`${baseUrl}/readyz`);
            const body = await response.json() as { ready?: boolean; conditions?: Record<string, boolean> };
            if (response.ok && body.ready) {
                return;
            }
            lastStatus = JSON.stringify(body);
        } catch (error) {
            lastStatus = error instanceof Error ? error.message : String(error);
        }
        await new Promise(resolve => setTimeout(resolve, 250));
    }

    throw new Error(`Timed out waiting for ${baseUrl}/readyz. Last status: ${lastStatus}`);
}

export interface ReadyzPayload {
    ready?: boolean;
    conditions?: Record<string, boolean>;
    directBuffer?: {
        requested?: boolean;
        supported?: boolean;
        active?: boolean;
        reason?: string;
        captureMode?: string;
        renderNode?: string;
        renderer?: string;
        screencopyAvailable?: boolean;
        linuxDmabufAvailable?: boolean;
    };
}

export async function fetchReadyz(baseUrl: string): Promise<ReadyzPayload> {
    const response = await fetch(`${baseUrl}/readyz`);
    return await response.json() as ReadyzPayload;
}

export interface ClientStats {
    fps: number;
    totalDecoded: number;
    bandwidth?: number;
    bytesReceived?: number;
    latency?: number;
}

export async function readClientStats(page: Page): Promise<ClientStats> {
    return await page.evaluate(() => {
        if (!(window as any).getStats) {
            return { fps: -1, totalDecoded: -1 };
        }
        return (window as any).getStats();
    }) as ClientStats;
}

export async function waitForStreamingFrames(page: Page, message: string, timeoutMs = 45000): Promise<void> {
    await expect.poll(async () => {
        const before = await readClientStats(page);
        await page.waitForTimeout(2000);
        const after = await readClientStats(page);

        return {
            beforeDecoded: before.totalDecoded,
            afterDecoded: after.totalDecoded,
            deltaDecoded: after.totalDecoded - before.totalDecoded,
            fps: after.fps,
        };
    }, {
        timeout: timeoutMs,
        message,
    }).toMatchObject({
        fps: expect.any(Number),
    });

    await expect.poll(async () => {
        const before = await readClientStats(page);
        await page.waitForTimeout(2000);
        const after = await readClientStats(page);
        return after.totalDecoded > before.totalDecoded && after.totalDecoded > 10 && after.fps > 0;
    }, {
        timeout: timeoutMs,
        message,
    }).toBe(true);
}
