import { log, statusEl, displayEl, sharpnessLayerEl, ctx, clientGpuCheckbox, applySmoothingSettings } from './ui';

export class WebCodecsManager {
    public totalDecoded = 0;
    public fps = 0;
    public latencyMonitor = 0;
    public videoCodec = 'vp8';
    public chroma = '420';

    private frameCount = 0;
    private lastFPSUpdate = Date.now();
    private decoder: VideoDecoder | null = null;
    private isInitializing = false;
    private decoderInitTimeout: ReturnType<typeof setTimeout> | null = null;

    private getIsWebRtcActive: () => boolean;
    private getNetworkLatency: () => number;
    private getWsBandwidthMbps: () => number;

    constructor(getIsWebRtcActive: () => boolean, getNetworkLatency: () => number, getWsBandwidthMbps: () => number) {
        this.getIsWebRtcActive = getIsWebRtcActive;
        this.getNetworkLatency = getNetworkLatency;
        this.getWsBandwidthMbps = getWsBandwidthMbps;
        this.initDecoder();
    }

    public pollStats() {
        const now = Date.now();
        const deltaMs = now - this.lastFPSUpdate;
        if (deltaMs >= 1000) {
            this.fps = Math.round((this.frameCount * 1000) / deltaMs);
            this.frameCount = 0;
            this.lastFPSUpdate = now;
        }
    }

    public initDecoder() {
        if (this.isInitializing) {
            if (this.decoderInitTimeout === null) {
                this.decoderInitTimeout = setTimeout(() => {
                    this.decoderInitTimeout = null;
                    this.initDecoder();
                }, 100);
            }
            return;
        }
        this.isInitializing = true;

        if (this.decoderInitTimeout !== null) {
            clearTimeout(this.decoderInitTimeout);
            this.decoderInitTimeout = null;
        }

        if (typeof VideoDecoder === 'undefined') {
            log('WebCodecs API not supported by this browser.');
            this.isInitializing = false;
            return;
        }

        if (this.decoder) {
            try {
                if (this.decoder.state !== 'closed') this.decoder.close();
            } catch (e: unknown) {
                console.warn('Error closing decoder:', (e as Error).message);
            }
        }

        try {
            this.decoder = new VideoDecoder({
                output: (frame) => this.handleFrame(frame),
                error: (e: Error) => {
                    log(`WebCodecs Decoder Error: ${e.message}`);
                    if (this.decoderInitTimeout === null) {
                        this.decoderInitTimeout = setTimeout(() => {
                            this.decoderInitTimeout = null;
                            this.initDecoder();
                        }, 2000);
                    }
                }
            });

            const isH264 = this.videoCodec.startsWith('h264');
            const isH265 = this.videoCodec.startsWith('h265');
            const isAV1 = this.videoCodec.startsWith('av1');
            
            let codecStr = 'vp8';
            if (isH264) {
                codecStr = (this.chroma === '444') ? 'avc1.F40032' : 'avc1.42E034';
            } else if (isH265) {
                codecStr = (this.chroma === '444') ? 'hev1.4.10.L150.B0' : 'hev1.1.6.L120.90';
            } else if (isAV1) {
                codecStr = (this.chroma === '444') ? 'av01.1.08M.08' : 'av01.0.08M.08';
            }

            const hardwareAcceleration = clientGpuCheckbox && clientGpuCheckbox.checked ? 'prefer-hardware' : 'prefer-software';
            const baseConfig: VideoDecoderConfig = {
                codec: codecStr,
                optimizeForLatency: true,
                hardwareAcceleration
            };

            if (isH265 && this.chroma === '444') {
                this.probeHEVC444(baseConfig).finally(() => {
                    this.isInitializing = false;
                });
            } else {
                this.configureDecoder(baseConfig);
                this.isInitializing = false;
            }

        } catch (e: unknown) {
            log('Failed to initialize decoder: ' + (e as Error).message);
            console.error(e);
            this.isInitializing = false;
        }
    }

    private configureDecoder(config: VideoDecoderConfig) {
        if (!this.decoder) return;
        try {
            this.decoder.configure(config);
            window.hasReceivedKeyFrame = false;
            log(`Decoder configured (${this.videoCodec}: ${config.codec}, chroma: ${this.chroma}). Waiting for Keyframe...`);
        } catch (e: unknown) {
            log(`Configuration failed: ${(e as Error).message}`);
        }
    }

    private async probeHEVC444(baseConfig: VideoDecoderConfig) {
        const candidates = [
            'hev1.4.10.L150.B0', // Profile 4 (Main 4:4:4 8), L5.0
            'hvc1.4.10.L150.B0',
            'hev1.6.10.L150.B0', // Profile 6 (Main 4:4:4 10), L5.0
            'hvc1.6.10.L150.B0',
            'hev1.2.4.L150.B0',  // Main 10
            'hvc1.2.4.L150.B0',
            'hev1.1.6.L150.B0',  // Main
        ];

        log(`Probing HEVC 4:4:4 support across ${candidates.length} codec strings...`);

        for (const codec of candidates) {
            const config = { ...baseConfig, codec };
            try {
                const res = await VideoDecoder.isConfigSupported(config);
                if (res.supported) {
                    log(`Found supported HEVC string: ${codec}`);
                    this.configureDecoder(res.config!);
                    return;
                }
            } catch {
                // Ignore error and try next
            }
        }

        log('None of the HEVC 4:4:4 codec strings are supported by this browser.');
        if (statusEl) statusEl.textContent = 'HEVC 4:4:4 Not Supported';
    }

    private handleFrame(frame: VideoFrame) {
        if (this.totalDecoded === 0) {
            log('First frame decoded successfully!');
            console.log('Frame Format:', frame.format, frame.codedWidth, frame.codedHeight);
        }
        this.totalDecoded++;

        if (ctx && frame.displayWidth && frame.displayHeight) {
            if (displayEl.width !== frame.displayWidth || displayEl.height !== frame.displayHeight) {
                displayEl.width = frame.displayWidth;
                displayEl.height = frame.displayHeight;
                if (sharpnessLayerEl) {
                    sharpnessLayerEl.width = frame.displayWidth;
                    sharpnessLayerEl.height = frame.displayHeight;
                }
                applySmoothingSettings();
            }
            ctx.drawImage(frame as CanvasImageSource, 0, 0, displayEl.width, displayEl.height);
        }

        frame.close();
        this.frameCount++;
    }

    public decodeChunk(isKey: boolean, timestamp: number, chunkData: Uint8Array) {
        if (this.decoder && this.decoder.state === 'configured') {
            try {
                this.decoder.decode(new EncodedVideoChunk({
                    type: isKey ? 'key' : 'delta',
                    timestamp: timestamp * 1000,
                    data: chunkData
                }));
            } catch (e: unknown) {
                console.error('Decode exception:', (e as Error).message);
                if (!this.isInitializing && this.decoderInitTimeout === null) {
                    this.initDecoder();
                }
            }
        } else if (this.decoder && (this.decoder.state === 'closed' || this.decoder.state === 'unconfigured')) {
            if (!this.isInitializing && this.decoderInitTimeout === null) {
                this.initDecoder();
            }
        }
    }
}
