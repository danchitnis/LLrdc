export const statusEl = document.getElementById('status') as HTMLDivElement;
export const displayContainerEl = document.getElementById('display-container') as HTMLDivElement;
export const displayEl = document.getElementById('display') as HTMLCanvasElement;
export const sharpnessLayerEl = document.getElementById('sharpness-layer') as HTMLCanvasElement;
export const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement;
export const overlayEl = document.getElementById('input-overlay') as HTMLDivElement;
export const clipboardArea = document.getElementById('clipboard-area') as HTMLTextAreaElement;
export const bandwidthSelect = document.getElementById('bandwidth-select') as HTMLSelectElement;
export const vbrCheckbox = document.getElementById('vbr-checkbox') as HTMLInputElement;
export const mpdecimateCheckbox = document.getElementById('mpdecimate-checkbox') as HTMLInputElement;
export const hybridCheckbox = document.getElementById('hybrid-checkbox') as HTMLInputElement;
export const settleSlider = document.getElementById('settle-slider') as HTMLInputElement;
export const settleValue = document.getElementById('settle-value') as HTMLSpanElement;
export const tileSizeSlider = document.getElementById('tile-size-slider') as HTMLInputElement;
export const tileSizeValue = document.getElementById('tile-size-value') as HTMLSpanElement;
export const keyframeIntervalSelect = document.getElementById('keyframe-interval-select') as HTMLSelectElement;

export const configBtn = document.getElementById('config-btn') as HTMLButtonElement;
export const configDropdown = document.getElementById('config-dropdown') as HTMLDivElement;
export const configTabBtns = document.querySelectorAll('.config-tab-btn') as NodeListOf<HTMLButtonElement>;
export const targetTypeRadios = document.getElementsByName('target-type') as NodeListOf<HTMLInputElement>;
export const qualitySlider = document.getElementById('quality-slider') as HTMLInputElement;
export const qualityValue = document.getElementById('quality-value') as HTMLSpanElement;
export const framerateSelect = document.getElementById('framerate-select') as HTMLSelectElement;
export const hdpiSelect = document.getElementById('hdpi-select') as HTMLSelectElement;
export const maxResSelect = document.getElementById('max-res-select') as HTMLSelectElement;

export const cpuEffortSlider = document.getElementById('cpu-effort-slider') as HTMLInputElement;
export const cpuEffortValue = document.getElementById('cpu-effort-value') as HTMLSpanElement;
export const cpuThreadsSelect = document.getElementById('cpu-threads-select') as HTMLSelectElement;
export const desktopMouseCheckbox = document.getElementById('desktop-mouse-checkbox') as HTMLInputElement;
export const videoCodecSelect = document.getElementById('video-codec-select') as HTMLSelectElement;
export const codecGpuOpts = document.querySelectorAll('.codec-opt-gpu') as NodeListOf<HTMLOptionElement>;
export const clientGpuCheckbox = document.getElementById('client-gpu-checkbox') as HTMLInputElement;
export const chromaCheckbox = document.getElementById('chroma-checkbox') as HTMLInputElement;
export const clipboardCheckbox = document.getElementById('clipboard-checkbox') as HTMLInputElement;

export const ctx = displayEl.getContext('2d', { alpha: false, desynchronized: true });
export const sharpnessCtx = sharpnessLayerEl ? sharpnessLayerEl.getContext('2d') : null;

export function log(msg: string) {
    console.log(msg);
}

export let serverFfmpegCpu = 0;

export function setServerFfmpegCpu(cpu: number) {
    serverFfmpegCpu = cpu;
}

export function updateStatusText(isWebRtcActive: boolean, fps: number, latencyMonitor: number, networkLatency: number, bandwidthMbps: number = 0, width: number = 0, height: number = 0, codec: string = 'vp8') {
    if (!statusEl) return;
    
    // Clean up codec name for display and check for GPU
    const isGpu = codec.includes('nvenc');
    const displayCodec = codec.replace('_nvenc', '');
    const gpuTag = isGpu ? ' 🚀 GPU' : '';
    
    const transportInfo = isWebRtcActive ? `[WebRTC ${displayCodec}${gpuTag}]` : `[WebCodecs ${displayCodec}${gpuTag}]`;
    const resInfo = (width > 0 && height > 0) ? ` | Res: ${width}x${height}` : '';
    
    // Change color based on latency
    let color = '#4f4'; // Green
    if (latencyMonitor > 150 || networkLatency > 100) {
        color = '#fa4'; // Orange
    }
    if (latencyMonitor > 300 || networkLatency > 200) {
        color = '#f44'; // Red
    }
    
    if (keyframeIntervalSelect) {
        keyframeIntervalSelect.disabled = !isWebRtcActive;
    }
    
    statusEl.style.color = color;
    statusEl.textContent = `${transportInfo}${resInfo} | FPS: ${fps} | Latency (Video): ${latencyMonitor}ms | Ping: ${networkLatency}ms | BW: ${bandwidthMbps.toFixed(2)} Mbps | FFmpeg CPU: ${Math.round(serverFfmpegCpu)}%`;
}
