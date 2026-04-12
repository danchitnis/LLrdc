import { test, expect } from '@playwright/test';
import { spawn } from 'child_process';
import { getContainerImage } from '../helpers';

const PORT = 8960 + Math.floor(Math.random() * 100);

test.describe('Wayland Intel H.265 Direct Rejection', () => {
    test('should reject Intel h265_qsv in direct-buffer mode instead of starting a dead stream', async () => {
        test.setTimeout(90000);
        const containerImage = getContainerImage('intel');

        const output = await new Promise<string>((resolve, reject) => {
            const proc = spawn('./docker-run.sh', ['--intel', '--direct-buffer', '--host-net', '--name', `llrdc-wayland-intel-h265-direct-${PORT}`], {
                env: {
                    ...process.env,
                    IMAGE_NAME: containerImage.name,
                    IMAGE_TAG: containerImage.tag,
                    PORT: PORT.toString(),
                    HOST_PORT: PORT.toString(),
                    VIDEO_CODEC: 'h265_qsv',
                },
                stdio: ['ignore', 'pipe', 'pipe'],
            });

            let combined = '';
            const onData = (data: Buffer) => {
                combined += data.toString();
            };

            proc.stdout?.on('data', onData);
            proc.stderr?.on('data', onData);

            proc.on('error', reject);
            proc.on('exit', (code) => {
                if (code === 0) {
                    reject(new Error(`Intel H.265 direct unexpectedly started successfully.\n${combined}`));
                    return;
                }
                resolve(combined);
            });
        });

        expect(output).toContain('Invalid direct-buffer configuration: Intel H.265 hardware encode is not supported on this FFmpeg/driver stack; use h264_qsv or av1_qsv for direct mode');
    });
});
