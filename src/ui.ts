export const statusEl = document.getElementById('status') as HTMLDivElement;
export const displayContainerEl = document.getElementById('display-container') as HTMLDivElement;
export const displayEl = document.getElementById('display') as HTMLCanvasElement;
export const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement;
export const overlayEl = document.getElementById('input-overlay') as HTMLDivElement;
export const bandwidthSelect = document.getElementById('bandwidth-select') as HTMLSelectElement;
export const vbrCheckbox = document.getElementById('vbr-checkbox') as HTMLInputElement;

export const configBtn = document.getElementById('config-btn') as HTMLButtonElement;
export const configDropdown = document.getElementById('config-dropdown') as HTMLDivElement;
export const configTabBtns = document.querySelectorAll('.config-tab-btn') as NodeListOf<HTMLButtonElement>;
export const targetTypeRadios = document.getElementsByName('target-type') as NodeListOf<HTMLInputElement>;
export const qualitySlider = document.getElementById('quality-slider') as HTMLInputElement;
export const qualityValue = document.getElementById('quality-value') as HTMLSpanElement;
export const framerateSelect = document.getElementById('framerate-select') as HTMLSelectElement;
export const maxResSelect = document.getElementById('max-res-select') as HTMLSelectElement;

export const cpuEffortSlider = document.getElementById('cpu-effort-slider') as HTMLInputElement;
export const cpuEffortValue = document.getElementById('cpu-effort-value') as HTMLSpanElement;
export const cpuThreadsSelect = document.getElementById('cpu-threads-select') as HTMLSelectElement;
export const desktopMouseCheckbox = document.getElementById('desktop-mouse-checkbox') as HTMLInputElement;

export const ctx = displayEl.getContext('2d', { alpha: false, desynchronized: true });

export function log(msg: string) {
    console.log(msg);
}

export function updateStatusText(isWebRtcActive: boolean, fps: number, latencyMonitor: number, networkLatency: number, bandwidthMbps: number = 0, width: number = 0, height: number = 0) {
    if (!statusEl) return;
    const codecInfo = isWebRtcActive ? '[WebRTC VP8]' : '[WebCodecs VP8]';
    const resInfo = (width > 0 && height > 0) ? ` | Res: ${width}x${height}` : '';
    statusEl.textContent = `${codecInfo}${resInfo} | FPS: ${fps} | Latency (Video): ${latencyMonitor}ms | Ping: ${networkLatency}ms | BW: ${bandwidthMbps.toFixed(2)} Mbps`;
}
