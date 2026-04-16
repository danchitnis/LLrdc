import { NetworkManager } from '../network';
import { WebCodecsManager } from '../webcodecs';
import { WebRTCManager } from '../webrtc';
import { ClientEventEmitter } from './hooks';
import { detectKeyFrame, parseBinaryVideoPacket } from './protocol';
import { updateStatusText, videoEl, displayEl } from '../ui';
import type { BrowserClientState, ConfigMessage, PresentedFrameMeta } from './types';

export interface BrowserClientEvents {
    connected: undefined;
    disconnected: undefined;
    serverMessage: Record<string, unknown>;
    presentedFrame: PresentedFrameMeta;
    error: string;
}

interface BrowserClientApi {
    getState: () => BrowserClientState;
    getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; bytesReceived: number; };
    getPresentedFrames: () => PresentedFrameMeta[];
    clearPresentedFrames: () => void;
    sendConfig: (config: ConfigMessage) => void;
    sendResize: (width: number, height: number) => void;
    sendInput: (data: string) => void;
}

declare global {
    interface Window {
        __llrdcClient?: BrowserClientApi;
        __llrdcLatestFrameMeta?: PresentedFrameMeta;
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; bytesReceived: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
        hardwareAccelerationAvailable: boolean;
        serverFfmpegFps?: number;
        webrtcManager: WebRTCManager;
        webcodecsManager: WebCodecsManager;
        networkManager: NetworkManager;
    }
}

const MAX_PRESENTED_FRAMES = 240;

export class BrowserClientSession {
    public readonly events = new ClientEventEmitter<BrowserClientEvents>();
    public readonly network: NetworkManager;
    public readonly webcodecs: WebCodecsManager;
    public readonly webrtc: WebRTCManager;

    private presentedFrames: PresentedFrameMeta[] = [];
    private masterPollInterval: ReturnType<typeof setInterval>;

    constructor() {
        this.webcodecs = new WebCodecsManager(
            () => this.webrtc ? this.webrtc.isWebRtcActive : false,
            () => this.network.networkLatency,
            () => this.network.wsBandwidthMbps
        );

        this.webrtc = new WebRTCManager(
            (data) => this.network.sendMsg(data),
            () => this.network.networkLatency,
            () => this.webcodecs.latencyMonitor,
            (frame) => this.recordPresentedFrame(frame)
        );

        this.network = new NetworkManager(
            (buffer) => this.handleBinaryMessage(buffer),
            (msg) => this.handleJsonMessage(msg),
            () => {
                this.webrtc.initWebRTC();
                this.events.emit('connected', undefined);
            }
        );

        window.networkManager = this.network;
        window.webcodecsManager = this.webcodecs;
        window.webrtcManager = this.webrtc;

        this.installWindowApi();

        this.masterPollInterval = setInterval(() => this.masterPollStats(), 1000);
    }

    private async masterPollStats() {
        // Poll managers for updated smoothed stats
        await this.webrtc.pollStats();
        this.webcodecs.pollStats();

        const webrtcActive = this.webrtc.isWebRtcActive;
        const iceConnected = this.webrtc.rtcPeer?.iceConnectionState === 'connected' || this.webrtc.rtcPeer?.iceConnectionState === 'completed';
        
        // We consider WebRTC "really" active only if it's flagged active AND ICE is connected
        const showWebRtc = webrtcActive && iceConnected;

        const fps = showWebRtc ? this.webrtc.fps : this.webcodecs.fps;
        const bw = showWebRtc ? this.webrtc.bandwidthMbps : this.network.wsBandwidthMbps;
        const codec = showWebRtc ? this.webrtc.videoCodec : this.webcodecs.videoCodec;
        
        // Final fallback for resolution if WebRTC video element is ready
        const width = showWebRtc && videoEl.videoWidth > 0 ? videoEl.videoWidth : displayEl.width;
        const height = showWebRtc && videoEl.videoHeight > 0 ? videoEl.videoHeight : displayEl.height;

        const networkLatency = this.network.networkLatency;
        const displayLatency = showWebRtc && this.webrtc.getCurrentLatency() > 0 
            ? this.webrtc.getCurrentLatency() 
            : this.webcodecs.latencyMonitor;

        // SINGLE POINT OF TRUTH for UI status bar
        updateStatusText(
            showWebRtc,
            fps,
            displayLatency,
            networkLatency,
            bw,
            width,
            height,
            codec
        );
    }

    public sendInput(data: string) {
        if (this.webrtc.inputChannel && this.webrtc.inputChannel.readyState === 'open') {
            this.webrtc.inputChannel.send(data);
            return;
        }
        this.network.sendMsg(data);
    }

    public sendConfig(config: ConfigMessage) {
        this.network.sendMsg(JSON.stringify(config));
    }

    public sendResize(width: number, height: number) {
        this.network.sendMsg(JSON.stringify({ type: 'resize', width, height }));
    }

    public getPresentedFrames(): PresentedFrameMeta[] {
        return this.presentedFrames.map(frame => ({ ...frame }));
    }

    public clearPresentedFrames() {
        this.presentedFrames = [];
    }

    public getStats() {
        const webrtcTotal = this.webrtc.lastTotalDecoded >= 0 ? this.webrtc.lastTotalDecoded : 0;
        const webcodecsTotal = this.webcodecs.totalDecoded >= 0 ? this.webcodecs.totalDecoded : 0;
        const useWebRtc = this.webrtc.isWebRtcActive;

        return {
            fps: useWebRtc ? this.webrtc.fps : this.webcodecs.fps,
            latency: this.webcodecs.latencyMonitor,
            totalDecoded: useWebRtc ? webrtcTotal : webcodecsTotal,
            webrtcFps: this.webrtc.fps,
            bytesReceived: useWebRtc ? this.webrtc.lastBytesReceived : this.network.totalBytesReceived,
        };
    }

    public getState(): BrowserClientState {
        return {
            wsConnected: this.network.wsConnected,
            webrtcActive: this.webrtc.isWebRtcActive,
            videoCodec: this.webrtc.isWebRtcActive ? this.webrtc.videoCodec : this.webcodecs.videoCodec,
            totalDecoded: this.getStats().totalDecoded,
            networkLatency: this.network.networkLatency,
            webrtcLatency: this.webrtc.getCurrentLatency(),
            webSocketBytesReceived: this.network.totalBytesReceived,
            lastPresentedFrame: this.presentedFrames.length > 0 ? { ...this.presentedFrames[this.presentedFrames.length - 1] } : null,
        };
    }

    private installWindowApi() {
        window.getStats = () => this.getStats();
        window.__llrdcClient = {
            getState: () => this.getState(),
            getStats: () => this.getStats(),
            getPresentedFrames: () => this.getPresentedFrames(),
            clearPresentedFrames: () => this.clearPresentedFrames(),
            sendConfig: (config) => this.sendConfig(config),
            sendResize: (width, height) => this.sendResize(width, height),
            sendInput: (data) => this.sendInput(data),
        };
    }

    private handleBinaryMessage(buffer: ArrayBuffer) {
        const packet = parseBinaryVideoPacket(buffer);
        if (!packet) {
            return;
        }

        const now = Date.now();
        this.webcodecs.latencyMonitor = Math.round(Math.abs(now - packet.timestampMs));

        const isKey = detectKeyFrame(this.webcodecs.videoCodec, packet.chunkData);
        if (isKey) {
            window.hasReceivedKeyFrame = true;
        }

        if (!window.hasReceivedKeyFrame || this.webrtc.isWebRtcActive) {
            return;
        }

        this.webcodecs.decodeChunk(isKey, packet.timestampMs, packet.chunkData);
    }

    private handleJsonMessage(msg: Record<string, unknown>) {
        if (msg.type === 'webrtc_answer') {
            this.webrtc.handleAnswer(msg.sdp as RTCSessionDescriptionInit);
            return;
        }

        if (msg.type === 'webrtc_ice' && msg.candidate) {
            this.webrtc.handleIce(msg.candidate as RTCIceCandidateInit);
            return;
        }

        this.events.emit('serverMessage', msg);
    }

    private recordPresentedFrame(frame: PresentedFrameMeta) {
        this.presentedFrames.push(frame);
        if (this.presentedFrames.length > MAX_PRESENTED_FRAMES) {
            this.presentedFrames.splice(0, this.presentedFrames.length - MAX_PRESENTED_FRAMES);
        }
        window.__llrdcLatestFrameMeta = frame;
        this.events.emit('presentedFrame', frame);
    }
}
