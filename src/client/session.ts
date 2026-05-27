import { WebCodecsManager } from '../webcodecs';
import { WebTransportManager } from '../webtransport';
import { WebSocketTransport } from '../websocket-transport';
import { ClientEventEmitter } from './hooks';
import { detectKeyFrame, parseBinaryVideoPacket } from './protocol';
import { updateStatusText, log } from '../ui';
import type { BrowserClientState, ConfigMessage, PresentedFrameMeta, BrowserStats, BrowserClientApi } from './types';

export interface BrowserClientEvents {
    connected: undefined;
    disconnected: undefined;
    serverMessage: Record<string, unknown>;
    presentedFrame: PresentedFrameMeta;
    error: string;
}

const MAX_PRESENTED_FRAMES = 240;

export class BrowserClientSession {
    public readonly events = new ClientEventEmitter<BrowserClientEvents>();
    public readonly webcodecs: WebCodecsManager;
    public readonly webtransport: WebTransportManager;
    public readonly websocket: WebSocketTransport;

    private presentedFrames: PresentedFrameMeta[] = [];
    private masterPollInterval: ReturnType<typeof setInterval>;
    private isSecureContext = (globalThis as any).isSecureContext;
    private isBootstrapping = false;
    private lastReceivedKeyFrameTime = 0;
    private lastKeyframeRequestTime = 0;

    constructor() {
        this.webcodecs = new WebCodecsManager();

        const onConnected = () => {
            this.events.emit('connected', undefined);
        };

        this.webtransport = new WebTransportManager(
            (buffer) => this.handleBinaryMessage(buffer),
            (msg) => this.handleJsonMessage(msg),
            onConnected
        );

        this.websocket = new WebSocketTransport(
            (buffer) => this.handleBinaryMessage(buffer),
            (msg) => this.handleJsonMessage(msg),
            onConnected
        );

        (window as any).webcodecsManager = this.webcodecs;
        (window as any).webtransportManager = this.webtransport;
        (window as any).websocketTransport = this.websocket;

        this.installWindowApi();

        this.masterPollInterval = setInterval(() => this.masterPollStats(), 1000);

        // Auto-reconnect if no transport is active
        setInterval(() => {
            if (!this.webtransport.isWebTransportActive && !this.webtransport.isConnecting && 
                !this.websocket.isActive && !this.websocket.isConnecting) {
                this.bootstrap();
            }
        }, 5000);

        this.bootstrap();
    }

    private async bootstrap() {
        if (this.isBootstrapping) return;
        this.isBootstrapping = true;

        try {
            const resp = await fetch('/config');
            if (resp.ok) {
                const config = await resp.json();
                this.handleJsonMessage(config);
            } else {
                log(`[Bootstrap] Config fetch failed: ${resp.status} ${resp.statusText}`);
            }
        } catch (e) {
            console.error(`[Bootstrap] Config fetch error: ${(e as Error).message}`);
        } finally {
            this.isBootstrapping = false;
        }
    }

    private masterPollStats() {
        this.webcodecs.pollStats();

        const fps = this.webcodecs.fps;
        const displayLatency = this.webcodecs.latencyMonitor;
        const codec = this.webcodecs.videoCodec;

        // Try to get resolution from WebCodecs first, then fall back to WebRTC video element
        let width = this.webcodecs.width;
        let height = this.webcodecs.height;
        
        const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement | null;
        if ((width === 0 || height === 0) && videoEl && videoEl.videoWidth > 0) {
            width = videoEl.videoWidth;
            height = videoEl.videoHeight;
        }

        const isWT = this.webtransport.isWebTransportActive;
        const isWS = this.websocket.isActive;
        const transportActive = isWT || isWS;
        const transportFps = isWT ? this.webtransport.fps : this.websocket.fps;

        updateStatusText(
            false,
            fps,
            displayLatency,
            0, // networkLatency
            0, // bandwidth
            width,
            height,
            codec,
            transportActive,
            transportFps,
            isWS // Added as a hint for status display
        );
    }

    public sendInput(data: string) {
        if (this.webtransport.isWebTransportActive) {
            this.webtransport.sendMsg(data);
        } else if (this.websocket.isActive) {
            this.websocket.sendMsg(data);
        }
    }

    public sendConfig(config: ConfigMessage) {
        if (this.webtransport.isWebTransportActive) {
            this.webtransport.sendMsg(JSON.stringify(config));
        } else if (this.websocket.isActive) {
            this.websocket.sendMsg(JSON.stringify(config));
        }
    }

    public sendResize(width: number, height: number, dpr?: number) {
        const msg = JSON.stringify({ type: 'resize', width, height, dpr });
        if (this.webtransport.isWebTransportActive) {
            this.webtransport.sendMsg(msg);
        } else if (this.websocket.isActive) {
            this.websocket.sendMsg(msg);
        }
    }

    public getPresentedFrames(): PresentedFrameMeta[] {
        return this.presentedFrames.map(frame => ({ ...frame }));
    }

    public clearPresentedFrames() {
        this.presentedFrames = [];
    }

    public getStats(): BrowserStats {
        const isWT = this.webtransport.isWebTransportActive;
        return {
            fps: this.webcodecs.fps,
            latency: this.webcodecs.latencyMonitor,
            totalDecoded: this.webcodecs.totalDecoded,
            bytesReceived: isWT ? this.webtransport.totalBytesReceived : this.websocket.totalBytesReceived,
            webtransportFps: isWT ? this.webtransport.fps : this.websocket.fps
        };
    }

    public getState(): BrowserClientState {
        return {
            wsConnected: this.websocket.isActive,
            webrtcActive: false,
            videoCodec: this.webcodecs.videoCodec,
            totalDecoded: this.webcodecs.totalDecoded,
            networkLatency: 0,
            webrtcLatency: 0,
            webSocketBytesReceived: this.websocket.totalBytesReceived,
            lastPresentedFrame: this.presentedFrames.length > 0 ? { ...this.presentedFrames[this.presentedFrames.length - 1] } : null,
        };
    }

    private installWindowApi() {
        (globalThis as any).getStats = () => this.getStats();
        (globalThis as any).__llrdcClient = {
            getState: () => this.getState(),
            getStats: () => this.getStats(),
            getPresentedFrames: () => this.getPresentedFrames(),
            clearPresentedFrames: () => this.clearPresentedFrames(),
            sendConfig: (config: ConfigMessage) => this.sendConfig(config),
            sendResize: (width: number, height: number) => this.sendResize(width, height),
            sendInput: (data: string) => this.sendInput(data),
        };
    }

    private handleBinaryMessage(buffer: ArrayBuffer) {
        const packet = parseBinaryVideoPacket(buffer);
        if (!packet) return;

        const now = Date.now();
        this.webcodecs.latencyMonitor = Math.round(Math.abs(now - packet.timestampMs));

        const isKey = detectKeyFrame(this.webcodecs.videoCodec, packet.chunkData);
        if (isKey) {
            (globalThis as any).hasReceivedKeyFrame = true;
            this.lastReceivedKeyFrameTime = now;
        }

        if (!(globalThis as any).hasReceivedKeyFrame) {
            // If we've been waiting for a keyframe for more than 2 seconds, ask the server for one
            if (now - this.lastKeyframeRequestTime > 2000) {
                log('[Transport] Waiting for keyframe... requesting one from server.');
                this.lastKeyframeRequestTime = now;
                this.sendInput(JSON.stringify({ type: 'force_keyframe' }));
            }
            return;
        }

        this.webcodecs.decodeChunk(isKey, packet.timestampMs, packet.chunkData);
    }

    private handleJsonMessage(msg: Record<string, unknown>) {
        if (msg.type === 'config') {
            if (msg.videoCodec) {
                this.webcodecs.videoCodec = msg.videoCodec as string;
            }
            if (msg.chroma) {
                this.webcodecs.chroma = msg.chroma as string;
            }
            this.webcodecs.initDecoder();

            const wtFingerprint = msg.webtransportFingerprint as string;
            const wtPort = msg.webtransportPort as number;

            if (wtFingerprint && wtPort) {
                const canUseWT = ('WebTransport' in (globalThis as any)) && this.isSecureContext;
                
                if (canUseWT) {
                    const wtUrl = `https://${(globalThis as any).location.hostname}:${wtPort}/webtransport`;
                    this.webtransport.connect(wtUrl, wtFingerprint);
                    
                    // Fallback to WS if WT connection hangs or fails quickly
                    setTimeout(() => {
                        if (!this.webtransport.isWebTransportActive && !this.websocket.isActive) {
                            log('[Transport] WebTransport taking too long, trying WebSocket fallback...');
                            const wsUrl = `ws://${(globalThis as any).location.hostname}:${(globalThis as any).location.port || 8080}/ws`;
                            this.websocket.connect(wsUrl);
                        }
                    }, 2000);
                } else {
                    const reason = this.isSecureContext ? 'Browser too old' : 'Non-secure context';
                    log(`[Transport] WebTransport not available (${reason}), falling back to WebSocket...`);
                    const wsUrl = `ws://${(globalThis as any).location.hostname}:${(globalThis as any).location.port || 8080}/ws`;
                    this.websocket.connect(wsUrl);
                }
            }
        }
        this.events.emit('serverMessage', msg);
    }

    private recordPresentedFrame(frame: PresentedFrameMeta) {
        this.presentedFrames.push(frame);
        if (this.presentedFrames.length > MAX_PRESENTED_FRAMES) {
            this.presentedFrames.splice(0, this.presentedFrames.length - MAX_PRESENTED_FRAMES);
        }
        (globalThis as any).__llrdcLatestFrameMeta = frame;
        this.events.emit('presentedFrame', frame);
    }
}
