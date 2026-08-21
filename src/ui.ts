export const statusEl = document.getElementById('status') as HTMLDivElement;
export const displayContainerEl = document.getElementById('display-container') as HTMLDivElement;
export const displayEl = document.getElementById('display') as HTMLCanvasElement;
export const sharpnessLayerEl = document.getElementById('sharpness-layer') as HTMLCanvasElement;
export const overlayEl = document.getElementById('input-overlay') as HTMLDivElement;
export const clipboardArea = document.getElementById('clipboard-area') as HTMLTextAreaElement;
export const bandwidthSelect = document.getElementById('bandwidth-select') as HTMLSelectElement;
export const vbrCheckbox = document.getElementById('vbr-checkbox') as HTMLInputElement;
export const vbrThresholdSlider = document.getElementById('vbr-threshold-slider') as HTMLInputElement;
export const vbrThresholdValue = document.getElementById('vbr-threshold-value') as HTMLSpanElement;
export const vbrThresholdGroup = document.getElementById('vbr-threshold-group') as HTMLDivElement;
export const damageTrackingCheckbox = document.getElementById('damage-tracking-checkbox') as HTMLInputElement;
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
export const nvencLatencyCheckbox = document.getElementById('nvenc-latency-checkbox') as HTMLInputElement;

export const desktopMouseCheckbox = document.getElementById('desktop-mouse-checkbox') as HTMLInputElement;
export const activityHzSlider = document.getElementById('activity-hz-slider') as HTMLInputElement;
export const activityHzValue = document.getElementById('activity-hz-value') as HTMLSpanElement;
export const activityTimeoutSlider = document.getElementById('activity-timeout-slider') as HTMLInputElement;
export const activityTimeoutValue = document.getElementById('activity-timeout-value') as HTMLSpanElement;
export const videoCodecSelect = document.getElementById('video-codec-select') as HTMLSelectElement;
export const codecGpuOpts = document.querySelectorAll('.codec-opt-gpu') as NodeListOf<HTMLOptionElement>;
export const directBufferStatusEl = document.getElementById('direct-buffer-status') as HTMLDivElement;
export const clientGpuCheckbox = document.getElementById('client-gpu-checkbox') as HTMLInputElement;
export const clipboardCheckbox = document.getElementById('clipboard-checkbox') as HTMLInputElement;

export const ctx = displayEl.getContext('2d', { alpha: false, desynchronized: true });
if (ctx) {
    ctx.imageSmoothingEnabled = false;
}
export const sharpnessCtx = sharpnessLayerEl ? sharpnessLayerEl.getContext('2d') : null;

export function applySmoothingSettings() {
    if (!displayEl || !displayContainerEl) return;

    const dpr = globalThis.devicePixelRatio || 1;
    const containerWidth = displayContainerEl.clientWidth * dpr;
    const containerHeight = displayContainerEl.clientHeight * dpr;

    const canvasWidth = displayEl.width;
    const canvasHeight = displayEl.height;

    let is1to1 = false;
    if (canvasWidth > 0 && canvasHeight > 0 && containerWidth > 0 && containerHeight > 0) {
        const scaleX = containerWidth / canvasWidth;
        const scaleY = containerHeight / canvasHeight;
        const scale = Math.min(scaleX, scaleY);
        // If scale is extremely close to 1.0 (e.g. 1% tolerance), it is effectively 1:1
        if (Math.abs(scale - 1.0) < 0.01) {
            is1to1 = true;
        }
    }

    if (is1to1) {
        displayEl.classList.add('crisp');
        if (sharpnessLayerEl) {
            sharpnessLayerEl.classList.add('crisp');
        }
        if (ctx) {
            ctx.imageSmoothingEnabled = false;
        }
        if (sharpnessCtx) {
            sharpnessCtx.imageSmoothingEnabled = false;
        }
    } else {
        displayEl.classList.remove('crisp');
        if (sharpnessLayerEl) {
            sharpnessLayerEl.classList.remove('crisp');
        }
        if (ctx) {
            ctx.imageSmoothingEnabled = true;
        }
        if (sharpnessCtx) {
            sharpnessCtx.imageSmoothingEnabled = true;
        }
    }
}

// Initial application
applySmoothingSettings();

export function log(msg: string) {
    console.log(msg);
}

export let serverFfmpegCpu = 0;
export let serverIntelGpuUtil = 0;
export let acceleratorMode: 'cpu' | 'intel' | 'nvidia' = 'cpu';
export let directBufferActive = false;

export function setServerFfmpegCpu(cpu: number) {
    serverFfmpegCpu = cpu;
}

export function setServerIntelGpuUtil(util: number) {
    serverIntelGpuUtil = util;
}

export function setAcceleratorMode(mode: 'cpu' | 'intel' | 'nvidia') {
    acceleratorMode = mode;
}

export function setDirectBufferActive(active: boolean) {
    directBufferActive = active;
}

export function updateStatusText(
    fps: number, 
    latencyMonitor: number, 
    pingMs: number, 
    bandwidthMbps: number = 0, 
    width: number = 0, 
    height: number = 0, 
    codec: string = 'vp8',
    isWebTransportActive: boolean = false,
    webtransportFps: number = 0,
    isWebSocket: boolean = false
) {
    if (!statusEl) return;
    
    // Clean up codec name for display and check for GPU
    const isGpu = codec.includes('nvenc') || codec.includes('vaapi');
    const displayCodec = codec.replace('_nvenc', '').replace('_vaapi', '');
    const gpuTag = isGpu ? ' 🚀 GPU' : '';
    
    let transport = 'WebCodecs';
    if (isWebTransportActive) {
        transport = isWebSocket ? 'WebSocket' : 'WebTransport';
    }
    
    // Change color based on latency
    let color = '#4f4'; // Green
    if (latencyMonitor > 150) {
        color = '#fa4'; // Orange
    }
    if (latencyMonitor > 300) {
        color = '#f44'; // Red
    }
    
    if (keyframeIntervalSelect) {
        keyframeIntervalSelect.disabled = !isWebTransportActive;
    }
    
    statusEl.style.color = color;
    statusEl.style.setProperty('--status-accent', color);
    
    // Condensed labels
    const displayRes = (width > 0 && height > 0) ? `${width}x${height} | ` : '';
    const displayFps = isWebTransportActive ? webtransportFps : fps;
    
    const directTag = directBufferActive ? ' ⚡ DIRECT' : '';
    let statsText = `[${transport} ${displayCodec}${gpuTag}${directTag}] ${displayRes}FPS: ${displayFps} | Lat: ${Math.round(latencyMonitor)}ms | Ping: ${Math.round(pingMs)}ms | BW: ${bandwidthMbps.toFixed(1)} | CPU: ${Math.round(serverFfmpegCpu)}%`;
    
    if (acceleratorMode === 'intel') {
        statsText += ` | Enc: ${Math.round(serverIntelGpuUtil)}%`;
    }
    
    statusEl.textContent = statsText;
}
