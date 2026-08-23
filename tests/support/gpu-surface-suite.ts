import { test, expect } from '@playwright/test';
import { fetchReadyz } from '../helpers';
import {
    assertInstalledHeadedChrome,
    assertStreamingConnection,
    navigateToHttpsViewer,
    resolveConnectionTarget,
    waitForConnectionReady,
} from './browser-connection';

export interface GpuSurfaceProfile {
    name: string;
    acceleratorMode: 'nvidia' | 'intel';
    codec: 'h264_nvenc' | 'h264_vaapi';
    useIntel: boolean;
    useNvidia: boolean;
    directBackend: 'nvidia-native' | 'intel-vaapi';
    defaultRenderNode?: string;
}

export const NVIDIA_GPU_PROFILE: GpuSurfaceProfile = {
    name: 'NVIDIA',
    acceleratorMode: 'nvidia',
    codec: 'h264_nvenc',
    useIntel: false,
    useNvidia: true,
    directBackend: 'nvidia-native',
};

export const INTEL_GPU_PROFILE: GpuSurfaceProfile = {
    name: 'Intel',
    acceleratorMode: 'intel',
    codec: 'h264_vaapi',
    useIntel: true,
    useNvidia: false,
    directBackend: 'intel-vaapi',
    defaultRenderNode: '/dev/dri/renderD130',
};

export function defineGpuConnectionSuite(profile: GpuSurfaceProfile): void {
    test.describe(`${profile.name} GPU browser connection`, () => {
        test('verifies the prestarted server and decoded H.264 stream', async ({ page }) => {
            test.setTimeout(120000);

            const loggedBrowserMessages = new Set<string>();
            page.on('console', message => {
                if (message.type() === 'error' || message.type() === 'warning' ||
                    /(Decoder|WebCodecs|First frame|Transport)/i.test(message.text())) {
                    const key = `${message.type()}:${message.text()}`;
                    if (!loggedBrowserMessages.has(key)) {
                        loggedBrowserMessages.add(key);
                        console.log(`[Browser ${message.type()}] ${message.text()}`);
                    }
                }
            });

            const target = resolveConnectionTarget();
            const expectedRenderNode = process.env.LLRDC_EXPECTED_RENDER_NODE || profile.defaultRenderNode;
            await assertInstalledHeadedChrome(page);
            await waitForConnectionReady(target);
            expect(target.expectedTransport).toBe('WebTransport');

            await expect.poll(() => fetchReadyz(target.serverUrl), {
                timeout: 30000,
                message: `Wait for ${profile.name} ${target.captureMode} readiness`,
            }).toMatchObject({
                ready: true,
                acceleratorMode: profile.acceleratorMode,
                videoCodec: profile.codec,
                captureMode: target.captureMode,
                useIntel: profile.useIntel,
                useNvidia: profile.useNvidia,
            });

            await navigateToHttpsViewer(page, target);
            await page.click('body');

            if (target.captureMode === 'compat') {
                await expect.poll(() => fetchReadyz(target.serverUrl), {
                    timeout: 30000,
                    message: 'Wait for compatibility capture state',
                }).toMatchObject({
                    directBuffer: {
                        requested: false,
                        active: false,
                        captureMode: 'compat',
                    },
                });
            } else {
                const directState: Record<string, unknown> = {
                    requested: true,
                    supported: true,
                    active: true,
                    captureMode: 'direct',
                    backend: profile.directBackend,
                    zeroCopyValidated: true,
                };
                if (expectedRenderNode) directState.renderNode = expectedRenderNode;

                await expect.poll(() => fetchReadyz(target.serverUrl), {
                    timeout: 45000,
                    message: `Wait for active ${profile.name} direct-buffer capture`,
                }).toMatchObject({ directBuffer: directState });
            }

            await assertStreamingConnection(
                page,
                target,
                `Wait for ${profile.name} ${target.captureMode} decoded H.264 frames`,
            );
        });
    });
}
