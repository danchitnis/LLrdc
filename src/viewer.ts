import { log, bandwidthSelect, vbrCheckbox, mpdecimateCheckbox, keyframeIntervalSelect, configBtn, configDropdown, targetTypeRadios, qualitySlider, qualityValue, framerateSelect, maxResSelect, displayContainerEl, overlayEl, configTabBtns, cpuEffortSlider, cpuEffortValue, cpuThreadsSelect, desktopMouseCheckbox, videoCodecSelect, codecGpuOpts, clientGpuCheckbox, clipboardCheckbox, setServerFfmpegCpu } from './ui';
import { NetworkManager } from './network';
import { WebCodecsManager } from './webcodecs';
import { WebRTCManager } from './webrtc';
import { setupInput, setPendingClipboard, setClipboardEnabled } from './input';

export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
        gpuAvailable: boolean;
    }
}

let triggerResizeUpdate: () => void = () => { };

// eslint-disable-next-line prefer-const
let webrtc: WebRTCManager;

const network = new NetworkManager(
    handleBinaryMessage,
    handleJsonMessage,
    () => {
        if (webrtc) webrtc.initWebRTC();
        triggerResizeUpdate();
    }
);

const webcodecs: WebCodecsManager = new WebCodecsManager(
    () => webrtc ? webrtc.isWebRtcActive : false,
    () => network.networkLatency,
    () => network.wsBandwidthMbps
);

webrtc = new WebRTCManager(
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
    mpdecimate?: boolean;
    keyframe_interval?: number;
    cpu_effort?: number;
    cpu_threads?: number;
    enable_desktop_mouse?: boolean;
    video_codec?: string;
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
    if (mpdecimateCheckbox) {
        config.mpdecimate = mpdecimateCheckbox.checked;
    }
    if (keyframeIntervalSelect) {
        config.keyframe_interval = parseInt(keyframeIntervalSelect.value, 10);
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

    if (videoCodecSelect) {
        config.video_codec = videoCodecSelect.value;
        
        if (webcodecs.videoCodec !== config.video_codec) {
            webcodecs.videoCodec = config.video_codec;
            webrtc.videoCodec = config.video_codec;
            webcodecs.initDecoder();
        }
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

if (mpdecimateCheckbox) {
    mpdecimateCheckbox.addEventListener('change', sendConfig);
}

if (keyframeIntervalSelect) {
    keyframeIntervalSelect.addEventListener('change', sendConfig);
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

if (maxResSelect) {
    maxResSelect.addEventListener('change', scheduleResize);
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

if (clipboardCheckbox) {
    clipboardCheckbox.addEventListener('change', () => {
        setClipboardEnabled(clipboardCheckbox.checked);
    });
}

if (videoCodecSelect) {
    videoCodecSelect.addEventListener('change', () => {
        if (cpuEffortSlider) {
            cpuEffortSlider.disabled = videoCodecSelect.value !== 'vp8';
        }
        sendConfig();
    });
}

if (clientGpuCheckbox) {
    clientGpuCheckbox.addEventListener('change', () => {
        webcodecs.initDecoder();
    });
}

let lastResizeWidth = 0;
let lastResizeHeight = 0;
let resizeTimer: number | null = null;

function sendResize() {
    if (!displayContainerEl) return;
    const rect = displayContainerEl.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) return;
    const scale = window.devicePixelRatio || 1;
    let width = Math.max(1, Math.round(rect.width * scale));
    let height = Math.max(1, Math.round(rect.height * scale));

    if (maxResSelect) {
        const maxCap = parseInt(maxResSelect.value, 10);
        if (maxCap > 0 && height > maxCap) {
            const ratio = maxCap / height;
            height = maxCap;
            width = Math.round(width * ratio);
        }
    }

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

        let isKey = false;
        if (webcodecs.videoCodec.startsWith('h264')) {
            // H.264 Annex B keyframe detection
            // Look for NAL unit type 5 (IDR) or 7 (SPS)
            for (let i = 0; i < chunkData.length - 4; i++) {
                if (chunkData[i] === 0 && chunkData[i + 1] === 0 && chunkData[i + 2] === 0 && chunkData[i + 3] === 1) {
                    const nalType = chunkData[i + 4] & 0x1F;
                    if (nalType === 5 || nalType === 7) {
                        isKey = true;
                        break;
                    }
                }
            }
        } else if (webcodecs.videoCodec.startsWith('av1')) {
            // AV1 keyframe detection
            // An AV1 keyframe (IDR) must contain a Sequence Header OBU (Type 1).
            // It often starts with a Temporal Delimiter OBU (Type 2).
            let pos = 0;
            while (pos < chunkData.length && pos < 100) { // Check first 100 bytes
                const obuType = (chunkData[pos] >> 3) & 0x0F;
                if (obuType === 1) { // Sequence Header
                    isKey = true;
                    break;
                }
                // Skip OBU header (1 byte) + extension header (optional 1 byte) + size (leb128)
                // This is complex to do fully, so we just check if the first or second OBU is Seq Header.
                // Most encoders put Temporal Delimiter (2 bytes usually: 0x12 0x00) then Seq Header.
                if (obuType === 2) { // Temporal Delimiter
                   pos += 2; // Usually 2 bytes
                   continue;
                }
                break;
            }
        } else {
            // VP8 keyframe detection
            isKey = (chunkData[0] & 0x01) === 0;
        }

        if (isKey) {
            window.hasReceivedKeyFrame = true;
        }

        if (!window.hasReceivedKeyFrame) return;
        if (webrtc && webrtc.isWebRtcActive) return;

        webcodecs.decodeChunk(isKey, timestamp, chunkData);
    }
}

function handleJsonMessage(msg: Record<string, unknown>) {
    if (msg.type === 'config') {
        if (msg.videoCodec && typeof msg.videoCodec === 'string') {
            log(`Server codec: ${msg.videoCodec}`);
            if (webcodecs.videoCodec !== msg.videoCodec) {
                webcodecs.videoCodec = msg.videoCodec;
                webrtc.videoCodec = msg.videoCodec;
                webcodecs.initDecoder();
            }

            if (msg.gpuAvailable !== undefined) {
                window.gpuAvailable = msg.gpuAvailable as boolean;
                if (codecGpuOpts) {
                    codecGpuOpts.forEach(opt => {
                        opt.style.display = msg.gpuAvailable ? '' : 'none';
                    });
                }
            }

            if (videoCodecSelect) {
                videoCodecSelect.value = msg.videoCodec as string;
                if (cpuEffortSlider) {
                    cpuEffortSlider.disabled = videoCodecSelect.value !== 'vp8';
                }
            }
        }
        
        if (msg.vbr !== undefined && vbrCheckbox) {
            vbrCheckbox.checked = msg.vbr as boolean;
        }
        
        if (msg.mpdecimate !== undefined && mpdecimateCheckbox) {
            mpdecimateCheckbox.checked = msg.mpdecimate as boolean;
        }
        
        if (msg.keyframe_interval !== undefined && keyframeIntervalSelect) {
            keyframeIntervalSelect.value = (msg.keyframe_interval as number).toString();
        }

        if (msg.enableClipboard !== undefined && clipboardCheckbox) {
            clipboardCheckbox.checked = msg.enableClipboard as boolean;
            setClipboardEnabled(msg.enableClipboard as boolean);
        }
    } else if (msg.type === 'clipboard_get') {
        if (typeof msg.text === 'string') {
            setPendingClipboard(msg.text);
        }
    } else if (msg.type === 'webrtc_answer') {
        webrtc.handleAnswer(msg.sdp as RTCSessionDescriptionInit);
    } else if (msg.type === 'webrtc_ice' && msg.candidate) {
        webrtc.handleIce(msg.candidate as RTCIceCandidateInit);
    } else if (msg.type === 'stats') {
        if (typeof msg.ffmpegCpu === 'number') {
            setServerFfmpegCpu(msg.ffmpegCpu);
        }
    } else if (msg.type === 'cursor_shape') {
        if (overlayEl && typeof msg.dataURL === 'string' && typeof msg.xhot === 'number' && typeof msg.yhot === 'number') {
            overlayEl.style.cursor = `url(${msg.dataURL}) ${msg.xhot} ${msg.yhot}, auto`;
        }
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
