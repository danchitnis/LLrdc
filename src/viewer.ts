import { 
    log, bandwidthSelect, vbrCheckbox, vbrThresholdSlider, vbrThresholdValue, vbrThresholdGroup, 
    damageTrackingCheckbox, mpdecimateCheckbox, hybridCheckbox, settleSlider, settleValue, 
    tileSizeSlider, tileSizeValue, keyframeIntervalSelect, targetTypeRadios, qualitySlider, 
    qualityValue, framerateSelect, hdpiSelect, maxResSelect, displayContainerEl, 
    cpuEffortSlider, cpuEffortValue, cpuThreadsSelect, nvencLatencyCheckbox, 
    desktopMouseCheckbox, activityHzSlider, activityHzValue, activityTimeoutSlider, 
    activityTimeoutValue, videoCodecSelect, clipboardCheckbox, 
    enableAudioCheckbox, audioBitrateSelect, setServerFfmpegCpu, 
    setServerIntelGpuUtil, setAcceleratorMode, applySmoothingSettings 
} from './ui';
import { WebCodecsManager } from './webcodecs';
import { setupInput, setPendingClipboard } from './input';
import { BrowserClientSession } from './client/session';
import type { ConfigMessage } from './client/types';
import { normalizeCodecFamily } from './client/protocol';
import { updateDirectBufferUi } from './direct-buffer-ui';
import { wireConfigControls } from './config-controls';
import { handleDisplayEffectMessage } from './display-effects';

export { };

let triggerResizeUpdate: () => void = () => { };

const session = new BrowserClientSession();
const webcodecs = (window as any).webcodecsManager as WebCodecsManager;

setupInput((data) => {
    session.sendInput(data);
});

session.events.on('connected', () => {
    triggerResizeUpdate();
});

let configDebounceTimer: ReturnType<typeof setTimeout> | null = null;
let deferredConfigTimer: ReturnType<typeof setTimeout> | null = null;
let currentHdpi = 100;
let pendingHdpi: number | null = null;
let pendingMaxRes: number | null = null;
let hasReceivedInitialConfig = false;

function buildConfigMessage(): ConfigMessage {
    const qualityStr = qualitySlider ? qualitySlider.value : '20';
    const bandwidthStr = bandwidthSelect ? bandwidthSelect.value : '5';
    const mode = Array.from(targetTypeRadios).find(r => r.checked)?.value || 'bandwidth';

    const videoCodec = videoCodecSelect ? videoCodecSelect.value : 'vp8';
    const chroma = (videoCodec.includes('444') || videoCodec.includes('-444')) ? '444' : '420';

    const config: ConfigMessage = {
        type: 'config',
        videoCodec,
        chroma,
        framerate: framerateSelect ? parseInt(framerateSelect.value, 10) : 30,
        vbr: vbrCheckbox ? vbrCheckbox.checked : false,
        vbr_threshold: vbrThresholdSlider ? parseInt(vbrThresholdSlider.value, 10) : 0,
        damageTracking: damageTrackingCheckbox ? damageTrackingCheckbox.checked : false,
        mpdecimate: mpdecimateCheckbox ? mpdecimateCheckbox.checked : false,
        keyframe_interval: keyframeIntervalSelect ? parseInt(keyframeIntervalSelect.value, 10) : 0,
        cpu_effort: cpuEffortSlider ? parseInt(cpuEffortSlider.value, 10) : 6,
        cpu_threads: cpuThreadsSelect ? parseInt(cpuThreadsSelect.value, 10) : 0,
        enable_desktop_mouse: desktopMouseCheckbox ? desktopMouseCheckbox.checked : true,
        settle_time: settleSlider ? parseInt(settleSlider.value, 10) : 500,
        tile_size: tileSizeSlider ? parseInt(tileSizeSlider.value, 10) : 128,
        enable_audio: enableAudioCheckbox ? enableAudioCheckbox.checked : true,
        audio_bitrate: audioBitrateSelect ? audioBitrateSelect.value : '128k',
        hdpi: hdpiSelect ? parseInt(hdpiSelect.value, 10) : 100,
        max_res: maxResSelect ? parseInt(maxResSelect.value, 10) : 0,
        activity_hz: activityHzSlider ? parseInt(activityHzSlider.value, 10) : 30,
        activity_timeout: activityTimeoutSlider ? parseInt(activityTimeoutSlider.value, 10) : 1500,
        nvenc_latency: nvencLatencyCheckbox ? nvencLatencyCheckbox.checked : true,
    };

    if (mode === 'bandwidth') {
        config.bandwidth = parseInt(bandwidthStr, 10);
    } else {
        config.quality = parseInt(qualityStr, 10);
    }

    return config;
}

function sendConfig() {
    if (!hasReceivedInitialConfig) {
        if (deferredConfigTimer !== null) clearTimeout(deferredConfigTimer);
        deferredConfigTimer = setTimeout(sendConfig, 100);
        return;
    }

    if (configDebounceTimer !== null) clearTimeout(configDebounceTimer);
    configDebounceTimer = setTimeout(() => {
        configDebounceTimer = null;
        session.sendConfig(buildConfigMessage());
    }, 50);
}

function sendConfigSync() {
    if (!hasReceivedInitialConfig) return;
    if (configDebounceTimer !== null) {
        clearTimeout(configDebounceTimer);
        configDebounceTimer = null;
    }
    session.sendConfig(buildConfigMessage());
}

session.events.on('serverMessage', (msg: any) => {
    if (msg.type === 'config') {
        if (msg.capabilities && msg.capabilities.valid_combinations && videoCodecSelect) {
            const browserCombos = msg.capabilities.valid_combinations.filter((combo: any) =>
                combo.supported_clients.includes('browser')
            );

            // Rebuild select options dynamically to only show those supported by server and browser client
            videoCodecSelect.innerHTML = '';
            browserCombos.forEach((combo: any) => {
                let encoderSuffix = combo.encoder;
                if (combo.encoder === 'intel') {
                    encoderSuffix = 'qsv';
                }

                let val = combo.codec;
                if (encoderSuffix !== 'cpu' && encoderSuffix !== 'macos') {
                    val += `_${encoderSuffix}`;
                }
                if (combo.chroma === '444') {
                    val += '-444';
                }

                let codecLabel = combo.codec.toUpperCase();
                if (combo.codec === 'h265') {
                    codecLabel = 'HEVC/H.265';
                } else if (combo.codec === 'h264') {
                    codecLabel = 'H.264';
                }

                let encoderLabel = 'CPU';
                if (combo.encoder === 'nvenc') {
                    encoderLabel = 'NVIDIA NVENC';
                } else if (combo.encoder === 'intel') {
                    encoderLabel = 'Intel VAAPI';
                } else if (combo.encoder === 'macos') {
                    encoderLabel = 'macOS VT';
                }

                let chromaLabel = '';
                if (combo.chroma === '444') {
                    chromaLabel = ' (4:4:4)';
                }

                const opt = document.createElement('option');
                opt.value = val;
                opt.textContent = `${codecLabel} (${encoderLabel})${chromaLabel}`;
                videoCodecSelect.appendChild(opt);
            });
        }

        if (msg.videoCodec && videoCodecSelect) {
            const serverCodec = msg.videoCodec as string;
            const chroma = msg.chroma as string;
            let targetValue = serverCodec;

            const options = Array.from(videoCodecSelect.options);

            // Special handling for 4:4:4 profiles to match UI values
            if (chroma === '444' && !targetValue.includes('444')) {
                if (targetValue.startsWith('h264')) {
                    if (targetValue.includes('_nvenc')) targetValue = 'h264_nvenc-444';
                    else targetValue = 'h264-444';
                } else if (targetValue.startsWith('h265') || targetValue.startsWith('hevc')) {
                    if (targetValue.includes('_nvenc')) targetValue = 'h265_nvenc-444';
                    else if (targetValue.includes('_qsv') || targetValue.includes('_vaapi')) targetValue = 'h265_qsv-444';
                    else targetValue = 'h265-444';
                }
            }

            // Standard mapping for _vaapi to _qsv for 4:2:0 profiles
            if (!options.some(opt => opt.value === targetValue)) {
                if (targetValue.includes('_vaapi')) {
                    const qsvMapped = targetValue.replace('_vaapi', '_qsv');
                    if (options.some(opt => opt.value === qsvMapped)) {
                        targetValue = qsvMapped;
                    }
                }
            }

            // Fallback to normalized family if still no match, otherwise use targetValue
            if (!options.some(opt => opt.value === targetValue)) {
                videoCodecSelect.value = normalizeCodecFamily(serverCodec);
            } else {
                videoCodecSelect.value = targetValue;
            }
        }
        if (msg.bandwidth !== undefined && bandwidthSelect) {
            bandwidthSelect.value = msg.bandwidth.toString();
        }
        if (msg.quality !== undefined && qualitySlider && qualityValue) {
            qualitySlider.value = msg.quality.toString();
            qualityValue.textContent = msg.quality.toString();
        }
        if (msg.vbr !== undefined && vbrCheckbox) {
            vbrCheckbox.checked = msg.vbr;
            if (vbrThresholdGroup) vbrThresholdGroup.style.display = msg.vbr ? 'flex' : 'none';
        }
        if (msg.vbr_threshold !== undefined && vbrThresholdSlider && vbrThresholdValue) {
            vbrThresholdSlider.value = msg.vbr_threshold.toString();
            vbrThresholdValue.textContent = msg.vbr_threshold.toString();
        }
        if (msg.damageTracking !== undefined && damageTrackingCheckbox) {
            damageTrackingCheckbox.checked = msg.damageTracking;
        }
        if (msg.mpdecimate !== undefined && mpdecimateCheckbox) {
            mpdecimateCheckbox.checked = msg.mpdecimate;
        }
        if (msg.keyframe_interval !== undefined && keyframeIntervalSelect) {
            keyframeIntervalSelect.value = msg.keyframe_interval.toString();
        }
        if (msg.framerate !== undefined && framerateSelect) {
            framerateSelect.value = msg.framerate.toString();
        }
        if (msg.cpu_effort !== undefined && cpuEffortSlider && cpuEffortValue) {
            cpuEffortSlider.value = msg.cpu_effort.toString();
            cpuEffortValue.textContent = msg.cpu_effort.toString();
        }
        if (msg.cpu_threads !== undefined && cpuThreadsSelect) {
            cpuThreadsSelect.value = msg.cpu_threads.toString();
        }
        if (msg.enable_desktop_mouse !== undefined && desktopMouseCheckbox) {
            desktopMouseCheckbox.checked = msg.enable_desktop_mouse;
        }
        if (msg.settle_time !== undefined && settleSlider && settleValue) {
            settleSlider.value = msg.settle_time.toString();
            settleValue.textContent = msg.settle_time.toString() + ' ms';
        }
        if (msg.tile_size !== undefined && tileSizeSlider && tileSizeValue) {
            tileSizeSlider.value = msg.tile_size.toString();
            tileSizeValue.textContent = msg.tile_size.toString() + ' px';
        }
        if (msg.activity_hz !== undefined && activityHzSlider && activityHzValue) {
            activityHzSlider.value = msg.activity_hz.toString();
            activityHzValue.textContent = msg.activity_hz.toString() + ' Hz';
        }
        if (msg.activity_timeout !== undefined && activityTimeoutSlider && activityTimeoutValue) {
            activityTimeoutSlider.value = msg.activity_timeout.toString();
            activityTimeoutValue.textContent = msg.activity_timeout.toString() + ' ms';
        }

        if (msg.enable_audio !== undefined && enableAudioCheckbox) {
            enableAudioCheckbox.checked = msg.enable_audio;
        }

        if (msg.enableClipboard !== undefined && clipboardCheckbox) {
            clipboardCheckbox.checked = msg.enableClipboard as boolean;
        }

        if (msg.audio_bitrate && audioBitrateSelect) {
            audioBitrateSelect.value = msg.audio_bitrate;
        }

        if (msg.hdpi !== undefined && hdpiSelect) {
            const displayHdpi = msg.hdpi <= 0 ? 100 : msg.hdpi;
            hdpiSelect.value = displayHdpi.toString();
            currentHdpi = displayHdpi;
        }

        if (msg.max_res !== undefined && maxResSelect) {
            maxResSelect.value = msg.max_res.toString();
        }

        if (msg.acceleratorMode) {
            setAcceleratorMode(msg.acceleratorMode);
        }

        if (msg.serverFfmpegCpu !== undefined) {
            setServerFfmpegCpu(msg.serverFfmpegCpu);
        }

        if (msg.serverIntelGpuUtil !== undefined) {
            setServerIntelGpuUtil(msg.serverIntelGpuUtil);
        }

        updateDirectBufferUi(msg);

        hasReceivedInitialConfig = true;
        pendingHdpi = null;
        pendingMaxRes = null;
    }

    if (msg.type === 'display_effect') {
        handleDisplayEffectMessage(msg, currentHdpi);
    }

    if (msg.type === 'clipboard_get') {
        if (typeof msg.text === 'string') {
            setPendingClipboard(msg.text);
        }
    }

    if (msg.type === 'stats') {
        if (msg.ffmpegCpu !== undefined) {
            setServerFfmpegCpu(msg.ffmpegCpu);
        }
        if (msg.intelGpuUtil !== undefined) {
            setServerIntelGpuUtil(msg.intelGpuUtil);
        }
    }
});

wireConfigControls({
    sendConfig,
    sendConfigSync,
    scheduleResize: () => triggerResizeUpdate(),
    reinitDecoder: () => webcodecs.initDecoder(),
    setPendingHdpi: (val) => { pendingHdpi = val; },
    setPendingMaxRes: (val) => { pendingMaxRes = val; }
});

let resizeDebounceTimer: ReturnType<typeof setTimeout> | null = null;
triggerResizeUpdate = () => {
    if (!displayContainerEl) return;
    applySmoothingSettings();
    if (resizeDebounceTimer !== null) clearTimeout(resizeDebounceTimer);
    
    resizeDebounceTimer = setTimeout(() => {
        resizeDebounceTimer = null;
        const dpr = globalThis.devicePixelRatio || 1;
        const width = Math.round(displayContainerEl.clientWidth * dpr);
        const height = Math.round(displayContainerEl.clientHeight * dpr);
        if (width > 0 && height > 0) {
            session.sendResize(width, height, dpr);
        }
    }, 250); // 250ms debounce
};

(globalThis as any).addEventListener('resize', () => {
    triggerResizeUpdate();
});

// Clipboard sync
if (clipboardCheckbox) {
    (globalThis as any).addEventListener('focus', async () => {
        if (!clipboardCheckbox.checked) return;
        try {
            const text = await navigator.clipboard.readText();
            if (text) {
                session.sendInput(JSON.stringify({ type: 'clipboard_set', text }));
            }
        } catch (e) {
            // Ignore clipboard errors
        }
    });
}

triggerResizeUpdate();
