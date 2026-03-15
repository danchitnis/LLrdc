import { log, bandwidthSelect, vbrCheckbox, mpdecimateCheckbox, keyframeIntervalSelect, configBtn, configDropdown, targetTypeRadios, qualitySlider, qualityValue, framerateSelect, hdpiSelect, maxResSelect, displayContainerEl, overlayEl, configTabBtns, cpuEffortSlider, cpuEffortValue, cpuThreadsSelect, desktopMouseCheckbox, videoCodecSelect, codecGpuOpts, clientGpuCheckbox, chromaCheckbox, clipboardCheckbox, setServerFfmpegCpu, videoEl } from './ui';
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
    chroma?: string;
    hdpi?: number;
}

let configDebounceTimer: number | null = null;

function sendConfig() {
    if (configDebounceTimer) {
        clearTimeout(configDebounceTimer);
    }
    
    configDebounceTimer = window.setTimeout(() => {
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
        if (hdpiSelect) {
            config.hdpi = parseInt(hdpiSelect.value, 10);
        }
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

        if (chromaCheckbox) {
            config.chroma = chromaCheckbox.checked ? '444' : '420';
        }

        if (videoCodecSelect) {
            config.video_codec = videoCodecSelect.value;
        }

        network.sendMsg(JSON.stringify(config));
        configDebounceTimer = null;
    }, 100);
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

if (chromaCheckbox) {
    chromaCheckbox.addEventListener('change', sendConfig);
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

if (hdpiSelect) {
    hdpiSelect.addEventListener('change', sendConfig);
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

let isReinitializingWebRTC = false;

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
window.addEventListener('load', () => {
    scheduleResize();
    const unmuteVideo = () => {
        if (videoEl && videoEl.muted) {
            videoEl.muted = false;
        }
    };
    window.addEventListener('mousedown', unmuteVideo, { once: true });
    window.addEventListener('keydown', unmuteVideo, { once: true });
});

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
        } else if (webcodecs.videoCodec.startsWith('h265')) {
            // H.265 Annex B keyframe detection
            // Look for VPS (32), SPS (33), PPS (34) or IDR (19, 20) or CRA (21)
            for (let i = 0; i < chunkData.length - 4; i++) {
                if (chunkData[i] === 0 && chunkData[i + 1] === 0 && chunkData[i + 2] === 0 && chunkData[i + 3] === 1) {
                    const nalType = (chunkData[i + 4] & 0x7E) >> 1;
                    if (nalType === 19 || nalType === 20 || nalType === 21 || nalType === 32 || nalType === 33 || nalType === 34) {
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
        let codecChanged = false;

        if (msg.videoCodec && typeof msg.videoCodec === 'string') {
            log(`Server codec: ${msg.videoCodec}`);
            if (webcodecs.videoCodec !== msg.videoCodec || webrtc.videoCodec !== msg.videoCodec) {
                webcodecs.videoCodec = msg.videoCodec;
                webrtc.videoCodec = msg.videoCodec;
                webcodecs.initDecoder();
                codecChanged = true;
            }
        }

        if (msg.framerate !== undefined && typeof msg.framerate === 'number') {
            if (framerateSelect) {
                framerateSelect.value = msg.framerate.toString();
            }
        }

        if (msg.hdpi !== undefined && typeof msg.hdpi === 'number') {
            if (hdpiSelect) {
                const displayHdpi = msg.hdpi === 0 ? 100 : msg.hdpi;
                hdpiSelect.value = displayHdpi.toString();
            }
        }

        if (webrtc.rtcPeer && (codecChanged || msg.restarted === true)) {
            log('Config change triggered FFmpeg restart, re-initializing WebRTC...');
            isReinitializingWebRTC = true;
            webrtc.initWebRTC();
            // Clear flag after 2 seconds or when WebRTC becomes active again
            setTimeout(() => { isReinitializingWebRTC = false; }, 2000);
        }

        if (msg.videoCodec && typeof msg.videoCodec === 'string') {
            if (msg.gpuAvailable !== undefined) {
                window.gpuAvailable = msg.gpuAvailable as boolean;
                if (codecGpuOpts) {
                    codecGpuOpts.forEach(opt => {
                        const isAV1 = opt.value === 'av1_nvenc';
                        const av1Available = msg.av1NvencAvailable as boolean;
                        
                        if (isAV1 && !av1Available) {
                            opt.style.display = 'none';
                        } else {
                            opt.style.display = msg.gpuAvailable ? '' : 'none';
                        }
                    });
                }
            }

            if (videoCodecSelect) {
                videoCodecSelect.value = msg.videoCodec as string;
                if (cpuEffortSlider) {
                    cpuEffortSlider.disabled = videoCodecSelect.value !== 'vp8';
                }
            }

            if (msg.h264Nvenc444Available !== undefined && chromaCheckbox && videoCodecSelect) {
                const updateChromaState = () => {
                    const isAV1Nvenc = videoCodecSelect.value === 'av1_nvenc';
                    const isH264Nvenc = videoCodecSelect.value === 'h264_nvenc';
                    const isH265Nvenc = videoCodecSelect.value === 'h265_nvenc';
                    
                    // AV1 NVENC never supports 444 (NVENC SDK limitation)
                    const codec_444_Missing = isAV1Nvenc || (isH264Nvenc && !msg.h264Nvenc444Available) || (isH265Nvenc && !msg.h265Nvenc444Available);
                    
                    if (codec_444_Missing) {
                        if (chromaCheckbox.checked) {
                            chromaCheckbox.checked = false;
                            sendConfig();
                        }
                        chromaCheckbox.disabled = true;
                        chromaCheckbox.parentElement!.style.opacity = '0.5';
                        chromaCheckbox.parentElement!.title = isAV1Nvenc
                            ? 'AV1 NVENC does not support 4:4:4 (NVENC SDK limitation)'
                            : '4:4:4 is not supported by your GPU hardware for this codec';
                    } else {
                        chromaCheckbox.disabled = false;
                        chromaCheckbox.parentElement!.style.opacity = '1';
                        chromaCheckbox.parentElement!.title = 'Improve text clarity by avoiding chroma subsampling (H.264/H.265/AV1 only)';
                    }
                };
                
                videoCodecSelect.addEventListener('change', updateChromaState);
                updateChromaState();
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

        if (msg.chroma && typeof msg.chroma === 'string') {
            log(`Server chroma: ${msg.chroma}`);
            if (webcodecs.chroma !== msg.chroma) {
                webcodecs.chroma = msg.chroma;
                webcodecs.initDecoder();
            }
            if (chromaCheckbox) {
                chromaCheckbox.checked = msg.chroma === '444';
            }
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
