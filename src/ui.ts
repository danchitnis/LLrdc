export const statusEl = document.getElementById('status') as HTMLDivElement;
export const displayEl = document.getElementById('display') as HTMLCanvasElement;
export const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement;
export const overlayEl = document.getElementById('input-overlay') as HTMLDivElement;
export const bandwidthSelect = document.getElementById('bandwidth-select') as HTMLSelectElement;

export const ctx = displayEl.getContext('2d', { alpha: false, desynchronized: true });

export function log(msg: string) {
    console.log(msg);
}

export function updateStatusText(isWebRtcActive: boolean, fps: number, latencyMonitor: number, networkLatency: number) {
    if (!statusEl) return;
    const codecInfo = isWebRtcActive ? '[WebRTC VP8]' : '[WebCodecs VP8]';
    statusEl.textContent = `${codecInfo} | FPS: ${fps} | Latency (Video): ${latencyMonitor}ms | Ping: ${networkLatency}ms`;
}
