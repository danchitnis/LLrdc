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
    webrtc_buffer?: number;
    nvenc_latency?: boolean;
    webrtc_low_latency?: boolean;
    activity_hz?: number;
    activity_timeout?: number;
    restarted?: boolean;
    captureMode?: string;
    directBufferRequested?: boolean;
    directBufferSupported?: boolean;
    directBufferActive?: boolean;
    directBufferReason?: string;
}

export interface PresentedFrameMeta {
    callbackAtMs: number;
    expectedDisplayAtMs: number | null;
    presentationAtMs: number | null;
    captureAtMs: number | null;
    receiveAtMs: number | null;
    processingDurationMs: number | null;
    presentedFrames: number | null;
}

export interface BrowserClientState {
    wsConnected: boolean;
    webrtcActive: boolean;
    videoCodec: string;
    totalDecoded: number;
    networkLatency: number;
    webrtcLatency: number;
    webSocketBytesReceived: number;
    lastPresentedFrame: PresentedFrameMeta | null;
}
