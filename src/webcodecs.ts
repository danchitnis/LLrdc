import { log, statusEl, displayEl, ctx, updateStatusText, clientGpuCheckbox } from './ui';

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

    public initDecoder() {
        if (this.isInitializing) return;
        this.isInitializing = true;

        if (this.decoderInitTimeout !== null) {
            clearTimeout(this.decoderInitTimeout);
            this.decoderInitTimeout = null;
        }

        if (typeof VideoDecoder === 'undefined') {
            if (!isSecureContext) {
                log('WebCodecs API requires a Secure Context (HTTPS or localhost). Falling back to WebRTC (if available)...');
                if (!this.getIsWebRtcActive() && statusEl) statusEl.textContent = 'WebCodecs: Requires HTTPS/localhost';
            } else {
                log('WebCodecs API not supported by this browser. Use Chrome or Edge.');
                if (!this.getIsWebRtcActive() && statusEl) statusEl.textContent = 'WebCodecs Not Supported';
            }
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
                    // Only show on UI if we aren't using WebRTC
                    if (!this.getIsWebRtcActive() && statusEl) {
                        statusEl.textContent = `Decoder Err: ${e.message}`;
                    }

                    if (this.decoderInitTimeout === null) {
                        this.decoderInitTimeout = setTimeout(() => {
                            this.decoderInitTimeout = null;
                            this.initDecoder();
                        }, 2000); // Wait longer before retry
                    }
                }
            });

            const isH264 = this.videoCodec.startsWith('h264');
            const isAV1 = this.videoCodec.startsWith('av1');
            
            let codecStr = 'vp8';
            if (isH264) {
                if (this.chroma === '444') {
                    // avc1.F40034 is High 4:4:4 Predictive profile, level 5.2
                    codecStr = 'avc1.F40034';
                } else {
                    // avc1.42E034 is Baseline profile, level 5.2 - supports 4K @ 120fps
                    codecStr = 'avc1.42E034';
                }
            } else if (isAV1) {
                if (this.chroma === '444') {
                    // av01.1.08M.08 - High profile, level 4.0, Main tier, 8-bit
                    codecStr = 'av01.1.08M.08';
                } else {
                    // av01.0.08M.08 - Main profile, level 4.0, Main tier, 8-bit
                    codecStr = 'av01.0.08M.08';
                }
            }

            const config: VideoDecoderConfig = {
                codec: codecStr,
                optimizeForLatency: true,
                hardwareAcceleration: clientGpuCheckbox && clientGpuCheckbox.checked ? 'prefer-hardware' : 'prefer-software'
            };

            this.decoder.configure(config);
            window.hasReceivedKeyFrame = false;
            log(`Decoder configured (${this.videoCodec}: ${codecStr}, chroma: ${this.chroma}). Waiting for Keyframe...`);
        } catch (e: unknown) {
            log('Failed to initialize decoder: ' + (e as Error).message);
            if (statusEl) statusEl.textContent = 'Decoder Init Error';
            console.error(e);
        } finally {
            this.isInitializing = false;
        }
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
            }
            ctx.drawImage(frame as CanvasImageSource, 0, 0, displayEl.width, displayEl.height);
        }

        frame.close();

        this.frameCount++;
        this.updateStats();
    }

    public updateStats() {
        const now = Date.now();
        if (now - this.lastFPSUpdate >= 1000) {
            this.fps = this.frameCount;
            this.frameCount = 0;
            this.lastFPSUpdate = now;
            updateStatusText(this.getIsWebRtcActive(), this.fps, this.latencyMonitor, this.getNetworkLatency(), this.getWsBandwidthMbps(), displayEl.width, displayEl.height, this.videoCodec);
        }
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
                if (statusEl) statusEl.textContent = 'Decode Exc: ' + (e as Error).message;
                if (!this.isInitializing && this.decoderInitTimeout === null) {
                    this.initDecoder();
                }
            }
        } else {
            if (this.decoder && (this.decoder.state === 'closed' || this.decoder.state === 'unconfigured')) {
                if (!this.isInitializing && this.decoderInitTimeout === null) {
                    log('Decoder stuck/closed. Re-initializing...');
                    this.initDecoder();
                }
            }
        }
    }
}
