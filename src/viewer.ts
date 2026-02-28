import { bandwidthSelect, vbrCheckbox, configBtn, configDropdown, targetTypeRadios, qualitySlider, qualityValue, framerateSelect, displayContainerEl, configTabBtns, cpuEffortSlider, cpuEffortValue, cpuThreadsSelect, desktopMouseCheckbox } from './ui';
import { NetworkManager } from './network';
import { WebCodecsManager } from './webcodecs';
import { WebRTCManager } from './webrtc';
import { setupInput } from './input';

export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
    }
}

let triggerResizeUpdate: () => void = () => { };

const network = new NetworkManager(
    handleBinaryMessage,
    handleJsonMessage,
    () => {
        webrtc.initWebRTC();
        triggerResizeUpdate();
    }
);

const webcodecs: WebCodecsManager = new WebCodecsManager(
    () => webrtc.isWebRtcActive,
    () => network.networkLatency,
    () => network.wsBandwidthMbps
);

const webrtc: WebRTCManager = new WebRTCManager(
    (data) => network.sendMsg(data),
    () => network.networkLatency,
    () => webcodecs.latencyMonitor
);

setupInput((data) => network.sendMsg(data));

interface ConfigMessage {
    type: 'config';
    bandwidth?: number;
    quality?: number;
    framerate?: number;
    vbr?: boolean;
    cpu_effort?: number;
    cpu_threads?: number;
    enable_desktop_mouse?: boolean;
}

function sendConfig() {
    let target = 'bandwidth';
    for (const radio of targetTypeRadios) {
        if (radio.checked) {
            target = radio.value;
            break;
        }
    }

    const config: ConfigMessage = { type: 'config' };
    if (target === 'bandwidth') {
        config.bandwidth = parseInt(bandwidthSelect.value, 10);
    } else {
        config.quality = parseInt(qualitySlider.value, 10);
    }
    config.framerate = parseInt(framerateSelect.value, 10);
    if (vbrCheckbox) {
        config.vbr = vbrCheckbox.checked;
    }
    if (cpuEffortSlider) {
        config.cpu_effort = parseInt(cpuEffortSlider.value, 10);
    }
    if (cpuThreadsSelect) {
        config.cpu_threads = parseInt(cpuThreadsSelect.value, 10);
    }
    if (desktopMouseCheckbox) {
        config.enable_desktop_mouse = desktopMouseCheckbox.checked;
    }

    network.sendMsg(JSON.stringify(config));
    if (webrtc.isWebRtcActive) {
        webrtc.initWebRTC();
    }
}

if (configBtn && configDropdown) {
    configBtn.addEventListener('click', () => {
        configDropdown.classList.toggle('hidden');
    });
}

for (const radio of targetTypeRadios) {
    radio.addEventListener('change', () => {
        const isBandwidth = radio.value === 'bandwidth';
        bandwidthSelect.disabled = !isBandwidth;
        if (vbrCheckbox) vbrCheckbox.disabled = !isBandwidth;
        qualitySlider.disabled = isBandwidth;
        sendConfig();
    });
}

if (bandwidthSelect) {
    bandwidthSelect.addEventListener('change', sendConfig);
}

if (vbrCheckbox) {
    vbrCheckbox.addEventListener('change', sendConfig);
}

if (qualitySlider && qualityValue) {
    qualitySlider.addEventListener('input', (e) => {
        qualityValue.textContent = (e.target as HTMLInputElement).value;
    });
    qualitySlider.addEventListener('change', sendConfig);
}

if (framerateSelect) {
    framerateSelect.addEventListener('change', sendConfig);
}

if (configTabBtns) {
    configTabBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            // Remove active class from all buttons and content
            configTabBtns.forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.config-tab-content').forEach(c => {
                (c as HTMLElement).style.display = 'none';
                c.classList.remove('active');
            });

            // Add active class to clicked button and target content
            btn.classList.add('active');
            const targetId = btn.getAttribute('data-tab');
            if (targetId) {
                const targetContent = document.getElementById(targetId);
                if (targetContent) {
                    targetContent.style.display = 'flex';
                    targetContent.classList.add('active');
                }
            }
        });
    });
}

if (cpuEffortSlider && cpuEffortValue) {
    cpuEffortSlider.addEventListener('input', (e) => {
        cpuEffortValue.textContent = (e.target as HTMLInputElement).value;
    });
    cpuEffortSlider.addEventListener('change', sendConfig);
}

if (cpuThreadsSelect) {
    cpuThreadsSelect.addEventListener('change', sendConfig);
}

if (desktopMouseCheckbox) {
    desktopMouseCheckbox.addEventListener('change', sendConfig);
}

let lastResizeWidth = 0;
let lastResizeHeight = 0;
let resizeTimer: number | null = null;

function sendResize() {
    if (!displayContainerEl) return;
    const rect = displayContainerEl.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) return;
    const scale = window.devicePixelRatio || 1;
    const width = Math.max(1, Math.round(rect.width * scale));
    const height = Math.max(1, Math.round(rect.height * scale));

    if (width === lastResizeWidth && height === lastResizeHeight) return;

    lastResizeWidth = width;
    lastResizeHeight = height;
    network.sendMsg(JSON.stringify({ type: 'resize', width, height }));
}

function scheduleResize() {
    if (resizeTimer !== null) {
        window.clearTimeout(resizeTimer);
    }
    resizeTimer = window.setTimeout(() => {
        resizeTimer = null;
        sendResize();
    }, 100);
}

triggerResizeUpdate = scheduleResize;

if (displayContainerEl && 'ResizeObserver' in window) {
    const observer = new ResizeObserver(() => scheduleResize());
    observer.observe(displayContainerEl);
}

window.addEventListener('resize', scheduleResize);
window.addEventListener('orientationchange', scheduleResize);
window.addEventListener('load', scheduleResize);

function handleBinaryMessage(buffer: ArrayBuffer) {
    const dv = new DataView(buffer);
    const type = dv.getUint8(0);

    if (type === 1) { // Video
        const timestamp = dv.getFloat64(1, false);
        const chunkData = new Uint8Array(buffer, 9);

        const now = Date.now();
        webcodecs.latencyMonitor = Math.round(Math.abs(now - timestamp));

        const isKey = (chunkData[0] & 0x01) === 0;
        if (isKey) {
            window.hasReceivedKeyFrame = true;
        }

        if (!window.hasReceivedKeyFrame) return;
        if (webrtc.isWebRtcActive) return;

        webcodecs.decodeChunk(isKey, timestamp, chunkData);
    }
}

function handleJsonMessage(msg: Record<string, unknown>) {
    if (msg.type === 'webrtc_answer') {
        webrtc.handleAnswer(msg.sdp as RTCSessionDescriptionInit);
    } else if (msg.type === 'webrtc_ice' && msg.candidate) {
        webrtc.handleIce(msg.candidate as RTCIceCandidateInit);
    }
}

window.getStats = () => {
    let webrtcTotal = 0;
    const v = document.getElementById('webrtc-video') as HTMLVideoElement;
    if (v && v.getVideoPlaybackQuality) {
        webrtcTotal = v.getVideoPlaybackQuality().totalVideoFrames;
    }
    return {
        fps: webrtc.isWebRtcActive ? webrtc.fps : webcodecs.fps,
        latency: webcodecs.latencyMonitor,
        totalDecoded: webrtc.isWebRtcActive ? webrtcTotal : webcodecs.totalDecoded,
        webrtcFps: webrtc.fps
    };
};
