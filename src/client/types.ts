import { WebCodecsManager } from '../webcodecs';
import { WebTransportManager } from '../webtransport';

export interface ConfigMessage {
    type: 'config';
    bandwidth?: number;
    quality?: number;
    max_res?: number;
    framerate?: number;
    vbr?: boolean;
    vbr_threshold?: number;
    damageTracking?: boolean;
    mpdecimate?: boolean;
    keyframe_interval?: number;
    cpu_effort?: number;
    cpu_threads?: number;
    enable_desktop_mouse?: boolean;
    videoCodec?: string;
    video_codec?: string;
    chroma?: string;
    hdpi?: number;
    enable_hybrid?: boolean;
    settle_time?: number;
    tile_size?: number;
    enable_audio?: boolean;
    audio_bitrate?: string;
    nvenc_latency?: boolean;
    activity_hz?: number;
    activity_timeout?: number;
    restarted?: boolean;
    captureMode?: string;
    directBufferRequested?: boolean;
    directBufferSupported?: boolean;
    directBufferActive?: boolean;
    directBufferReason?: string;
    h264Nvenc444Available?: boolean;
    h265Nvenc444Available?: boolean;
    webtransportFingerprint?: string;
    webtransportPort?: number;
}

export interface PresentedFrameMeta {
    callbackAtMs: number;
    expectedDisplayAtMs: number | null;
    presentationAtMs: number | null;
    captureAtMs: number | null;
    receiveAtMs: number | null;
    processingDurationMs: number | null;
    presentedFrames: number | null;
    rtpTimestamp?: number;
}

export interface BrowserClientState {
    wsConnected: boolean;
    webtransportActive: boolean;
    videoCodec: string;
    totalDecoded: number;
    networkLatency: number;
    webSocketBytesReceived: number;
    lastPresentedFrame: PresentedFrameMeta | null;
}

export interface BrowserStats {
    fps: number;
    latency: number;
    totalDecoded: number;
    bytesReceived: number;
    webtransportFps: number;
}

export interface BrowserClientApi {
    getState: () => BrowserClientState;
    getStats: () => BrowserStats;
    getPresentedFrames: () => PresentedFrameMeta[];
    clearPresentedFrames: () => void;
    sendConfig: (config: ConfigMessage) => void;
    sendResize: (width: number, height: number) => void;
    sendInput: (data: string) => void;
}
