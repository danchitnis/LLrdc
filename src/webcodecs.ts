import { log, statusEl, displayEl, ctx, updateStatusText } from './ui';

export class WebCodecsManager {
    public totalDecoded = 0;
    public fps = 0;
    public latencyMonitor = 0;

    private frameCount = 0;
    private lastFPSUpdate = Date.now();
    private decoder: VideoDecoder | null = null;
    private isInitializing = false;
    private decoderInitTimeout: ReturnType<typeof setTimeout> | null = null;

    private getIsWebRtcActive: () => boolean;
    private getNetworkLatency: () => number;

    constructor(getIsWebRtcActive: () => boolean, getNetworkLatency: () => number) {
        this.getIsWebRtcActive = getIsWebRtcActive;
        this.getNetworkLatency = getNetworkLatency;
        this.initDecoder();
    }

    public initDecoder() {
        if (this.isInitializing) return;
        this.isInitializing = true;

        if (this.decoderInitTimeout !== null) {
            clearTimeout(this.decoderInitTimeout);
            this.decoderInitTimeout = null;
        }

        if (!('VideoDecoder' in window)) {
            log('WebCodecs API not supported. Use Chrome or Edge.');
            if (statusEl) statusEl.textContent = 'WebCodecs Not Supported';
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
                    log(`Decoder Error: ${e.message}`);
                    console.error('VideoDecoder Error Details:', e);
                    if (statusEl) statusEl.textContent = `Decoder Err: ${e.message}`;

                    if (this.decoderInitTimeout === null) {
                        this.decoderInitTimeout = setTimeout(() => {
                            this.decoderInitTimeout = null;
                            this.initDecoder();
                        }, 100);
                    }
                }
            });

            this.decoder.configure({
                codec: 'vp8',
                optimizeForLatency: true,
                hardwareAcceleration: 'prefer-software'
            });

            window.hasReceivedKeyFrame = false;
            log('Decoder initialized (vp8). Waiting for Keyframe...');
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
            updateStatusText(this.getIsWebRtcActive(), this.fps, this.latencyMonitor, this.getNetworkLatency());
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
