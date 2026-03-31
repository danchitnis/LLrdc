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
