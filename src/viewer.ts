import { log, bandwidthSelect, vbrCheckbox, mpdecimateCheckbox, hybridCheckbox, settleSlider, settleValue, tileSizeSlider, tileSizeValue, keyframeIntervalSelect, configBtn, configDropdown, targetTypeRadios, qualitySlider, qualityValue, framerateSelect, hdpiSelect, maxResSelect, displayContainerEl, overlayEl, configTabBtns, cpuEffortSlider, cpuEffortValue, cpuThreadsSelect, desktopMouseCheckbox, videoCodecSelect, codecGpuOpts, clientGpuCheckbox, chromaCheckbox, clipboardCheckbox, enableAudioCheckbox, audioBitrateSelect, setServerFfmpegCpu, videoEl, sharpnessLayerEl, sharpnessCtx } from './ui';
import { NetworkManager } from './network';
import { WebCodecsManager } from './webcodecs';
import { WebRTCManager } from './webrtc';
import { setupInput, setPendingClipboard, setClipboardEnabled } from './input';

export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; bytesReceived: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
        gpuAvailable: boolean;
        webrtcManager: WebRTCManager;
        webcodecsManager: WebCodecsManager;
        networkManager: NetworkManager;
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
window.networkManager = network;

const webcodecs: WebCodecsManager = new WebCodecsManager(
    () => webrtc ? webrtc.isWebRtcActive : false,
    () => network.networkLatency,
    () => network.wsBandwidthMbps
);
window.webcodecsManager = webcodecs;

webrtc = new WebRTCManager(
    (data) => network.sendMsg(data),
    () => network.networkLatency,
    () => webcodecs.latencyMonitor
);
window.webrtcManager = webrtc;

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
    enable_hybrid?: boolean;
    settle_time?: number;
    tile_size?: number;
    enable_audio?: boolean;
    audio_bitrate?: string;
}

let configDebounceTimer: number | null = null;
let currentHdpi = 100;

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

        if (hybridCheckbox) {
            config.enable_hybrid = hybridCheckbox.checked;
        }

        if (settleSlider) {
            config.settle_time = parseInt(settleSlider.value, 10);
        }

        if (tileSizeSlider) {
            config.tile_size = parseInt(tileSizeSlider.value, 10);
        }

        if (videoCodecSelect) {
            config.video_codec = videoCodecSelect.value;
        }

        if (enableAudioCheckbox) {
            config.enable_audio = enableAudioCheckbox.checked;
        }

        if (audioBitrateSelect) {
            config.audio_bitrate = audioBitrateSelect.value;
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

function updateHybridSlidersState() {
    if (hybridCheckbox) {
        if (settleSlider) settleSlider.disabled = !hybridCheckbox.checked;
        if (tileSizeSlider) tileSizeSlider.disabled = !hybridCheckbox.checked;
    }
}

if (hybridCheckbox) {
    hybridCheckbox.addEventListener('change', () => {
        updateHybridSlidersState();
        sendConfig();
    });
    updateHybridSlidersState();
}

if (settleSlider && settleValue) {
    settleSlider.addEventListener('input', (e) => {
        settleValue.textContent = (e.target as HTMLInputElement).value;
    });
    settleSlider.addEventListener('change', sendConfig);
}

if (tileSizeSlider && tileSizeValue) {
    tileSizeSlider.addEventListener('input', (e) => {
        tileSizeValue.textContent = (e.target as HTMLInputElement).value;
    });
    tileSizeSlider.addEventListener('change', sendConfig);
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

if (enableAudioCheckbox) {
    enableAudioCheckbox.addEventListener('change', sendConfig);
}

if (audioBitrateSelect) {
    audioBitrateSelect.addEventListener('change', sendConfig);
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
    if (!displayContainerEl) { console.log('sendResize abort: no container'); return; }
    const rect = displayContainerEl.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) { console.log('sendResize abort: rect too small', rect); return; }
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

    console.log(`sendResize evaluated: w=${width}, h=${height}, lastW=${lastResizeWidth}, lastH=${lastResizeHeight}, connected=${network.wsConnected}`);

    if (width === lastResizeWidth && height === lastResizeHeight) return;

    if (!network.wsConnected) return; // Wait until network is connected to send and save state

    lastResizeWidth = width;
    lastResizeHeight = height;
    console.log(`Sending resize: ${width}x${height}`);
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

export function clearLosslessCanvas(x?: number, y?: number, w?: number, h?: number) {
    if (sharpnessCtx && sharpnessLayerEl) {
        if (x !== undefined && y !== undefined && w !== undefined && h !== undefined) {
            sharpnessCtx.clearRect(x, y, w, h);
        } else {
            sharpnessCtx.clearRect(0, 0, sharpnessLayerEl.width, sharpnessLayerEl.height);
        }
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
            currentHdpi = msg.hdpi === 0 ? 100 : msg.hdpi;
            if (hdpiSelect) {
                hdpiSelect.value = currentHdpi.toString();
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

        if (msg.enable_hybrid !== undefined && hybridCheckbox) {
            hybridCheckbox.checked = msg.enable_hybrid as boolean;
            updateHybridSlidersState();
        }

        if (msg.settle_time !== undefined && msg.settle_time !== null && settleSlider && settleValue) {
            settleSlider.value = (msg.settle_time as number).toString();
            settleValue.textContent = msg.settle_time.toString();
        }

        if (msg.tile_size !== undefined && msg.tile_size !== null && tileSizeSlider && tileSizeValue) {
            tileSizeSlider.value = (msg.tile_size as number).toString();
            tileSizeValue.textContent = msg.tile_size.toString();
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

        if (msg.enable_audio !== undefined && enableAudioCheckbox) {
            enableAudioCheckbox.checked = msg.enable_audio as boolean;
        }

        if (msg.audio_bitrate && typeof msg.audio_bitrate === 'string' && audioBitrateSelect) {
            audioBitrateSelect.value = msg.audio_bitrate;
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
    } else if (msg.type === 'lossless_patch') {
        if (sharpnessCtx && msg.data && typeof msg.data === 'string' && typeof msg.x === 'number' && typeof msg.y === 'number') {
            const img = new Image();
            img.onload = () => {
                sharpnessCtx!.drawImage(img, msg.x as number, msg.y as number);
            };
            img.src = msg.data;
        }
    } else if (msg.type === 'clear_lossless') {
        if (msg.rects && Array.isArray(msg.rects)) {
            for (const rect of msg.rects) {
                clearLosslessCanvas(rect.x as number, rect.y as number, rect.w as number, rect.h as number);
            }
        } else {
            clearLosslessCanvas(msg.x as number | undefined, msg.y as number | undefined, msg.w as number | undefined, msg.h as number | undefined);
        }
    } else if (msg.type === 'cursor_shape') {
        const shape = msg.shape as string;
        if (overlayEl && typeof msg.dataURL === 'string' && typeof msg.xhot === 'number' && typeof msg.yhot === 'number') {
            const dataURL = msg.dataURL;
            const xhot = msg.xhot;
            const yhot = msg.yhot;
            const img = new Image();
            img.onload = () => {
                const hdpiScale = currentHdpi / 100;
                const baseWidth = img.width / hdpiScale;
                const baseHeight = img.height / hdpiScale;
                
                const MIN_SIZE = 24;
                let scale = 1 / hdpiScale;
                
                if (baseWidth > 0 && baseHeight > 0 && (baseWidth < MIN_SIZE || baseHeight < MIN_SIZE)) {
                    const minScale = Math.max(MIN_SIZE / baseWidth, MIN_SIZE / baseHeight);
                    scale = scale * minScale;
                }

                // Use a small epsilon to avoid precision issues
                if (img.width > 0 && img.height > 0 && Math.abs(scale - 1.0) > 0.01) {
                    const newWidth = Math.round(img.width * scale);
                    const newHeight = Math.round(img.height * scale);
                    const newXhot = Math.round(xhot * scale);
                    const newYhot = Math.round(yhot * scale);

                    const canvas = document.createElement('canvas');
                    canvas.width = newWidth;
                    canvas.height = newHeight;
                    const ctx = canvas.getContext('2d');
                    if (ctx) {
                        ctx.imageSmoothingEnabled = true;
                        ctx.drawImage(img, 0, 0, newWidth, newHeight);
                        overlayEl.style.cursor = `url(${canvas.toDataURL('image/png')}) ${newXhot} ${newYhot}, auto`;
                        return;
                    }
                }
                overlayEl.style.cursor = `url(${dataURL}) ${xhot} ${yhot}, auto`;
            };
            img.src = dataURL;
        }
    }
}

window.getStats = () => {
    console.log('[getStats-DEBUG] Entering getStats');
    const webrtcTotal = (webrtc && webrtc.lastTotalDecoded >= 0) ? webrtc.lastTotalDecoded : 0;
    const webcodecsTotal = (webcodecs && webcodecs.totalDecoded >= 0) ? webcodecs.totalDecoded : 0;
    
    // If WebRTC is active, prefer its stats. Otherwise use WebCodecs.
    const useWebRtc = webrtc && webrtc.isWebRtcActive;
    
    return {
        fps: useWebRtc ? webrtc.fps : webcodecs.fps,
        latency: webcodecs.latencyMonitor,
        totalDecoded: useWebRtc ? webrtcTotal : webcodecsTotal,
        webrtcFps: webrtc ? webrtc.fps : 0,
        bytesReceived: useWebRtc ? webrtc.lastBytesReceived : network.totalBytesReceived
    };
};
