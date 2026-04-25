import { log, bandwidthSelect, vbrCheckbox, vbrThresholdSlider, vbrThresholdValue, vbrThresholdGroup, damageTrackingCheckbox, mpdecimateCheckbox, hybridCheckbox, settleSlider, settleValue, tileSizeSlider, tileSizeValue, keyframeIntervalSelect, targetTypeRadios, qualitySlider, framerateSelect, hdpiSelect, maxResSelect, displayContainerEl, cpuEffortSlider, cpuThreadsSelect, webrtcBufferSlider, webrtcBufferValue, nvencLatencyCheckbox, webrtcLowLatencyCheckbox, desktopMouseCheckbox, activityHzSlider, activityHzValue, activityTimeoutSlider, activityTimeoutValue, videoCodecSelect, codecGpuOpts, chromaCheckbox, clipboardCheckbox, enableAudioCheckbox, audioBitrateSelect, setServerFfmpegCpu, setServerIntelGpuUtil, setAcceleratorMode, videoEl } from './ui';
import { NetworkManager } from './network';
import { WebCodecsManager } from './webcodecs';
import { WebRTCManager } from './webrtc';
import { setupInput } from './input';
import { BrowserClientSession } from './client/session';
import type { ConfigMessage } from './client/types';
import { normalizeCodecFamily } from './client/protocol';
import { updateDirectBufferUi } from './direct-buffer-ui';
import { handleDisplayEffectMessage } from './display-effects';
import { updateHybridSlidersState, wireConfigControls } from './config-controls';

export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; bytesReceived: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
        hardwareAccelerationAvailable: boolean;
        serverFfmpegFps?: number;
        webrtcManager: WebRTCManager;
        webcodecsManager: WebCodecsManager;
        networkManager: NetworkManager;
    }
}

let triggerResizeUpdate: () => void = () => { };

const session = new BrowserClientSession();
const network = session.network;
const webcodecs = session.webcodecs;
const webrtc = session.webrtc;

session.events.on('connected', () => {
    triggerResizeUpdate();
});

session.events.on('serverMessage', (msg) => {
    handleJsonMessage(msg);
});

setupInput((data) => {
    session.sendInput(data);
});

let configDebounceTimer: number | null = null;
let deferredConfigTimer: number | null = null;
let currentHdpi = 100;
let hasReceivedInitialConfig = false;
let pendingHdpi: number | null = null;
let pendingMaxRes: number | null = null;

function sendConfig() {
    if (isReinitializingWebRTC) {
        if (deferredConfigTimer) {
            window.clearTimeout(deferredConfigTimer);
        }
        deferredConfigTimer = window.setTimeout(() => {
            deferredConfigTimer = null;
            sendConfig();
        }, 250);
        return;
    }
    if (deferredConfigTimer) {
        window.clearTimeout(deferredConfigTimer);
        deferredConfigTimer = null;
    }
    if (configDebounceTimer) {
        clearTimeout(configDebounceTimer);
    }

    const config = buildConfigMessage();
    
    configDebounceTimer = window.setTimeout(() => {
        session.sendConfig(config);
        configDebounceTimer = null;
    }, 100);
}

function buildConfigMessage(): ConfigMessage {
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
    if (maxResSelect) {
        config.max_res = parseInt(maxResSelect.value, 10);
    }
    if (vbrCheckbox) {
        config.vbr = vbrCheckbox.checked;
    }
    if (vbrThresholdSlider) {
        config.vbr_threshold = parseInt(vbrThresholdSlider.value, 10);
    }
    if (damageTrackingCheckbox) {
        config.damageTracking = damageTrackingCheckbox.checked;
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
        config.videoCodec = videoCodecSelect.value;
    }

    if (enableAudioCheckbox) {
        config.enable_audio = enableAudioCheckbox.checked;
    }

    if (audioBitrateSelect) {
        config.audio_bitrate = audioBitrateSelect.value;
    }

    if (webrtcBufferSlider) {
        config.webrtc_buffer = parseInt(webrtcBufferSlider.value, 10);
    }

    if (nvencLatencyCheckbox) {
        config.nvenc_latency = nvencLatencyCheckbox.checked;
    }

    if (webrtcLowLatencyCheckbox) {
        config.webrtc_low_latency = webrtcLowLatencyCheckbox.checked;
    }

    if (activityHzSlider) {
        config.activity_hz = parseInt(activityHzSlider.value, 10);
    }

    if (activityTimeoutSlider) {
        config.activity_timeout = parseInt(activityTimeoutSlider.value, 10);
    }

    return config;
}

wireConfigControls({
    sendConfig,
    scheduleResize,
    reinitDecoder: () => webcodecs.initDecoder(),
    setPendingHdpi: (value) => { pendingHdpi = value; },
    setPendingMaxRes: (value) => { pendingMaxRes = value; },
});

let lastResizeWidth = 0;
let lastResizeHeight = 0;
let resizeTimer: number | null = null;

let isReinitializingWebRTC = false;

function sendResize() {
    if (!displayContainerEl) { console.log('sendResize abort: no container'); return; }
    if (!hasReceivedInitialConfig) { console.log('sendResize abort: waiting for initial config'); return; }
    const rect = displayContainerEl.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) { console.log('sendResize abort: rect too small', rect); return; }
    const scale = window.devicePixelRatio || 1;
    let width = Math.max(1, Math.round(rect.width * scale));
    let height = Math.max(1, Math.round(rect.height * scale));

    if (maxResSelect) {
        const maxCap = parseInt(maxResSelect.value, 10);
        if (maxCap > 0) {
            // Fixed Resolution Mode: Force the vertical resolution to match the user's selection.
            // We maintain the aspect ratio of the viewer container to avoid stretching,
            // ensuring the remote desktop content is "exactly" the requested height (e.g., 1080p).
            const containerWidth = rect.width;
            const containerHeight = rect.height;
            const ratio = containerWidth / containerHeight;
            
            height = maxCap;
            width = Math.round(height * ratio);
            
            // If we are in a fixed height mode (e.g., 720p, 1080p), and the container ratio
            // is "reasonably" widescreen (between 1.5 and 2.1), snap to standard 16:9 widths.
            // This fulfills the user expectation of "exactly 1080p" (1920x1080).
            if (ratio > 1.2) {
                if (maxCap === 720) width = 1280;
                else if (maxCap === 1080) width = 1920;
                else if (maxCap === 1440) width = 2560;
                else if (maxCap === 2160) width = 3840;
            }
        } else if (height > 2160) {
            // Responsive Mode with a safety cap for 4K+.
            const ratio = 2160 / height;
            height = 2160;
            width = Math.round(width * ratio);
        }
    }

    console.log(`sendResize evaluated: w=${width}, h=${height}, lastW=${lastResizeWidth}, lastH=${lastResizeHeight}, connected=${network.wsConnected}`);

    if (width === lastResizeWidth && height === lastResizeHeight) return;

    if (!network.wsConnected) return; // Wait until network is connected to send and save state

    lastResizeWidth = width;
    lastResizeHeight = height;
    console.log(`Sending resize: ${width}x${height}`);
    session.sendResize(width, height);
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

let gpuOptionsList: HTMLOptionElement[] = [];

if (codecGpuOpts) {
    gpuOptionsList = Array.from(codecGpuOpts);
}

function handleJsonMessage(msg: Record<string, unknown>) {
    if (msg.type === 'config') {
        const firstConfig = !hasReceivedInitialConfig;
        hasReceivedInitialConfig = true;
        let codecChanged = false;
        updateDirectBufferUi(msg);

        if (msg.videoCodec && typeof msg.videoCodec === 'string') {
            log(`Server codec: ${msg.videoCodec}`);
            const normalizedNew = normalizeCodecFamily(msg.videoCodec);
            const normalizedWebCodecs = normalizeCodecFamily(webcodecs.videoCodec);
            const normalizedWebRTC = normalizeCodecFamily(webrtc.videoCodec);

            if (normalizedWebCodecs !== normalizedNew || normalizedWebRTC !== normalizedNew) {
                webcodecs.videoCodec = msg.videoCodec;
                webrtc.videoCodec = msg.videoCodec;
                
                // Only re-init WebCodecs decoder if we are actually using it
                if (!webrtc.isWebRtcActive) {
                    webcodecs.initDecoder();
                }
                codecChanged = true;
            }
        }

        if (msg.framerate !== undefined && typeof msg.framerate === 'number') {
            log(`Server framerate: ${msg.framerate} FPS`);
            window.serverFfmpegFps = msg.framerate;
            if (framerateSelect) {
                framerateSelect.value = msg.framerate.toString();
            }
        }

        if (msg.hdpi !== undefined && typeof msg.hdpi === 'number') {
            currentHdpi = msg.hdpi === 0 ? 100 : msg.hdpi;
            if (hdpiSelect) {
                if (pendingHdpi !== null && currentHdpi !== pendingHdpi) {
                    // Keep the optimistic local selection until the server echoes it back.
                } else {
                    hdpiSelect.value = currentHdpi.toString();
                    pendingHdpi = null;
                }
            }
        }

        if (msg.max_res !== undefined && typeof msg.max_res === 'number' && maxResSelect) {
            if (pendingMaxRes !== null && msg.max_res !== pendingMaxRes) {
                // Ignore stale config echoes while a local max-res change is still pending.
            } else {
                maxResSelect.value = msg.max_res.toString();
                pendingMaxRes = null;
            }
        }

        if (firstConfig) {
            scheduleResize();
        }

        if (webrtc.rtcPeer && codecChanged) {
            log('Codec change triggered WebRTC re-initialization...');
            isReinitializingWebRTC = true;
            webrtc.initWebRTC();
            // Clear flag after 2 seconds or when WebRTC becomes active again
            setTimeout(() => { isReinitializingWebRTC = false; }, 2000);
        }

        if (msg.restarted === true) {
            log('Server stream restarted');
        }

        if (msg.videoCodec && typeof msg.videoCodec === 'string') {
            if (typeof msg.acceleratorMode === 'string' && (msg.acceleratorMode === 'cpu' || msg.acceleratorMode === 'intel' || msg.acceleratorMode === 'nvidia')) {
                setAcceleratorMode(msg.acceleratorMode);
            }

            if (msg.hardwareAvailable !== undefined || msg.qsvAvailable !== undefined || msg.nvidiaAvailable !== undefined) {
                const anyGpuAvailable = (msg.hardwareAvailable as boolean) || (msg.qsvAvailable as boolean) || (msg.nvidiaAvailable as boolean);
                window.hardwareAccelerationAvailable = anyGpuAvailable;
                
                const hardwareOnlyElements = document.querySelectorAll('.hardware-only') as NodeListOf<HTMLElement>;
                hardwareOnlyElements.forEach(el => {
                    if (anyGpuAvailable) {
                        el.style.removeProperty('display');
                    } else {
                        el.style.setProperty('display', 'none', 'important');
                    }
                });

                const nvidiaOnlyElements = document.querySelectorAll('.nvidia-only') as NodeListOf<HTMLElement>;
                const nvidiaAvailable = msg.nvidiaAvailable === true;
                nvidiaOnlyElements.forEach(el => {
                    if (nvidiaAvailable) {
                        el.style.removeProperty('display');
                    } else {
                        el.style.setProperty('display', 'none', 'important');
                    }
                });

                if (videoCodecSelect && gpuOptionsList.length > 0) {
                    const nvencAvailable = msg.nvidiaAvailable as boolean;
                    const av1NvencAvailable = msg.av1NvencAvailable as boolean;
                    const qsvAvailable = msg.qsvAvailable as boolean;
                    const h265QsvAvailable = msg.h265QsvAvailable !== false;
                    const av1QsvAvailable = msg.av1QsvAvailable as boolean;
                    
                    gpuOptionsList.forEach(opt => {
                        const isNVENC = opt.value.endsWith('_nvenc');
                        const isQSV = opt.value.endsWith('_qsv');
                        const isAV1 = opt.value.startsWith('av1');
                        const isH265 = opt.value.startsWith('h265');
                        
                        let shouldShow = false;
                        if (isNVENC) {
                            shouldShow = nvencAvailable && (!isAV1 || av1NvencAvailable);
                        } else if (isQSV) {
                            shouldShow = qsvAvailable && (!isAV1 || av1QsvAvailable) && (!isH265 || h265QsvAvailable);
                        }
                        
                        if (shouldShow) {
                            if (!Array.from(videoCodecSelect.options).includes(opt)) {
                                videoCodecSelect.appendChild(opt);
                            }
                        } else {
                            if (Array.from(videoCodecSelect.options).includes(opt)) {
                                videoCodecSelect.removeChild(opt);
                            }
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
                    const isDirectMode = msg.captureMode === 'direct';
                    const isAV1Nvenc = videoCodecSelect.value === 'av1_nvenc';
                    const isH264Nvenc = videoCodecSelect.value === 'h264_nvenc';
                    const isH265Nvenc = videoCodecSelect.value === 'h265_nvenc';
                    
                    // AV1 NVENC never supports 444 (NVENC SDK limitation)
                    const codec_444_Missing = isDirectMode || isAV1Nvenc || (isH264Nvenc && !msg.h264Nvenc444Available) || (isH265Nvenc && !msg.h265Nvenc444Available);
                    
                    if (codec_444_Missing) {
                        if (chromaCheckbox.checked) {
                            chromaCheckbox.checked = false;
                            sendConfig();
                        }
                        chromaCheckbox.disabled = true;
                        chromaCheckbox.parentElement!.style.opacity = '0.5';
                        chromaCheckbox.parentElement!.title = isDirectMode
                            ? 'Direct capture mode currently requires YUV 4:2:0'
                            : isAV1Nvenc
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
            if (vbrThresholdGroup) vbrThresholdGroup.style.display = vbrCheckbox.checked ? 'flex' : 'none';
        }

        if (msg.vbr_threshold !== undefined && vbrThresholdSlider) {
            vbrThresholdSlider.value = (msg.vbr_threshold as number).toString();
            if (vbrThresholdValue) vbrThresholdValue.textContent = vbrThresholdSlider.value;
        }

        if (msg.damageTracking !== undefined && damageTrackingCheckbox) {
            damageTrackingCheckbox.checked = msg.damageTracking as boolean;
        }

        if (msg.mpdecimate !== undefined && mpdecimateCheckbox) {            mpdecimateCheckbox.checked = msg.mpdecimate as boolean;
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

        }

        if (msg.enable_audio !== undefined && enableAudioCheckbox) {
            enableAudioCheckbox.checked = msg.enable_audio as boolean;
        }

        if (msg.audio_bitrate && typeof msg.audio_bitrate === 'string' && audioBitrateSelect) {
            audioBitrateSelect.value = msg.audio_bitrate;
        }

        if (msg.webrtc_buffer !== undefined && msg.webrtc_buffer !== null && webrtcBufferSlider && webrtcBufferValue) {
            webrtcBufferSlider.value = (msg.webrtc_buffer as number).toString();
            webrtcBufferValue.textContent = msg.webrtc_buffer.toString();
        }

        if (msg.nvenc_latency !== undefined && msg.nvenc_latency !== null && nvencLatencyCheckbox) {
            nvencLatencyCheckbox.checked = msg.nvenc_latency as boolean;
        }

        if (msg.webrtc_low_latency !== undefined && msg.webrtc_low_latency !== null && webrtcLowLatencyCheckbox) {
            webrtcLowLatencyCheckbox.checked = msg.webrtc_low_latency as boolean;
        }

        if (msg.activity_hz !== undefined && msg.activity_hz !== null && activityHzSlider && activityHzValue) {
            activityHzSlider.value = (msg.activity_hz as number).toString();
            activityHzValue.textContent = msg.activity_hz.toString();
        }

        if (msg.activity_timeout !== undefined && msg.activity_timeout !== null && activityTimeoutSlider && activityTimeoutValue) {
            activityTimeoutSlider.value = (msg.activity_timeout as number).toString();
            activityTimeoutValue.textContent = msg.activity_timeout.toString();
        }
    } else if (msg.type === 'clipboard_get') {
        if (typeof msg.text === 'string') {
            log('Clipboard sync response received.');
        }
    } else if (msg.type === 'webrtc_answer') {
        webrtc.handleAnswer(msg.sdp as RTCSessionDescriptionInit);
    } else if (msg.type === 'webrtc_ice' && msg.candidate) {
        webrtc.handleIce(msg.candidate as RTCIceCandidateInit);
    } else if (msg.type === 'stats') {
        if (typeof msg.ffmpegCpu === 'number') {
            setServerFfmpegCpu(msg.ffmpegCpu);
        }
        if (typeof msg.intelGpuUtil === 'number') {
            setServerIntelGpuUtil(msg.intelGpuUtil);
        }
    } else if (handleDisplayEffectMessage(msg, currentHdpi)) {
        return;
    }
}

window.getStats = () => session.getStats();
