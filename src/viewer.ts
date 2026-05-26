import { log, bandwidthSelect, vbrCheckbox, vbrThresholdSlider, vbrThresholdValue, vbrThresholdGroup, damageTrackingCheckbox, mpdecimateCheckbox, hybridCheckbox, settleSlider, settleValue, tileSizeSlider, tileSizeValue, keyframeIntervalSelect, targetTypeRadios, qualitySlider, framerateSelect, hdpiSelect, maxResSelect, displayContainerEl, cpuEffortSlider, cpuThreadsSelect, nvencLatencyCheckbox, desktopMouseCheckbox, activityHzSlider, activityHzValue, activityTimeoutSlider, activityTimeoutValue, videoCodecSelect, codecGpuOpts, clipboardCheckbox, enableAudioCheckbox, audioBitrateSelect, setServerFfmpegCpu, setServerIntelGpuUtil, setAcceleratorMode } from './ui';
import { WebCodecsManager } from './webcodecs';
import { setupInput } from './input';
import { BrowserClientSession } from './client/session';
import { WebTransportManager } from './webtransport';
import type { ConfigMessage, BrowserStats } from './client/types';
import { normalizeCodecFamily } from './client/protocol';
import { updateDirectBufferUi } from './direct-buffer-ui';
import { handleDisplayEffectMessage } from './display-effects';
import { updateHybridSlidersState, wireConfigControls } from './config-controls';

export { };

let triggerResizeUpdate: () => void = () => { };

const session = new BrowserClientSession();
const webcodecs = session.webcodecs;

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

(globalThis as any).sendConfig = sendConfig;
(globalThis as any).buildConfigMessage = buildConfigMessage;

function sendConfig() {
    if (deferredConfigTimer) {
        (globalThis as any).clearTimeout(deferredConfigTimer);
        deferredConfigTimer = null;
    }
    if (configDebounceTimer) {
        clearTimeout(configDebounceTimer);
    }

    const config = buildConfigMessage();
    
    configDebounceTimer = (globalThis as any).setTimeout(() => {
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

    config.videoCodec = videoCodecSelect.value;
    config.chroma = (document.querySelector('input[name="chroma"]:checked') as HTMLInputElement)?.value || '420';
    config.framerate = parseInt(framerateSelect.value, 10);
    config.cpu_effort = parseInt(cpuEffortSlider.value, 10);
    config.cpu_threads = parseInt(cpuThreadsSelect.value, 10);
    config.vbr = vbrCheckbox.checked;
    config.vbr_threshold = parseInt(vbrThresholdSlider.value, 10);
    config.damageTracking = damageTrackingCheckbox.checked;
    config.mpdecimate = mpdecimateCheckbox.checked;
    config.enable_hybrid = hybridCheckbox.checked;
    config.settle_time = parseInt(settleSlider.value, 10);
    config.tile_size = parseInt(tileSizeSlider.value, 10);
    config.keyframe_interval = parseInt(keyframeIntervalSelect.value, 10);
    config.nvenc_latency = nvencLatencyCheckbox.checked;
    config.enable_desktop_mouse = desktopMouseCheckbox.checked;
    config.activity_hz = parseInt(activityHzSlider.value, 10);
    config.activity_timeout = parseInt(activityTimeoutSlider.value, 10);
    config.hdpi = parseInt(hdpiSelect.value, 10);
    config.max_res = parseInt(maxResSelect.value, 10);
    config.enable_audio = enableAudioCheckbox.checked;
    config.audio_bitrate = audioBitrateSelect.value;

    return config;
}

function handleJsonMessage(msg: Record<string, any>) {
    if (msg.type === 'config') {
        if (msg.videoCodec) {
            videoCodecSelect.value = msg.videoCodec;
            codecGpuOpts.forEach(opt => {
                opt.disabled = !msg.videoCodec.includes('nvenc') && !msg.videoCodec.includes('qsv') && msg.videoCodec !== 'hevc_vaapi';
            });
            if (cpuEffortSlider) {
                cpuEffortSlider.disabled = videoCodecSelect.value !== 'vp8';
            }
        }

        if (msg.vbr !== undefined && vbrCheckbox) {
            vbrCheckbox.checked = msg.vbr;
        }
        if (msg.vbr_threshold !== undefined && vbrThresholdSlider) {
            vbrThresholdSlider.value = msg.vbr_threshold.toString();
            vbrThresholdValue.textContent = msg.vbr_threshold.toString();
        }
        if (vbrThresholdGroup) {
            vbrThresholdGroup.style.display = vbrCheckbox.checked ? 'flex' : 'none';
        }

        if (msg.damageTracking !== undefined && damageTrackingCheckbox) {
            damageTrackingCheckbox.checked = msg.damageTracking;
        }
        if (msg.mpdecimate !== undefined && mpdecimateCheckbox) {
            mpdecimateCheckbox.checked = msg.mpdecimate;
        }

        if (msg.enable_hybrid !== undefined && hybridCheckbox) {
            hybridCheckbox.checked = msg.enable_hybrid;
        }
        if (msg.settle_time !== undefined && settleSlider) {
            settleSlider.value = msg.settle_time.toString();
            settleValue.textContent = msg.settle_time.toString() + 'ms';
        }
        if (msg.tile_size !== undefined && tileSizeSlider) {
            tileSizeSlider.value = msg.tile_size.toString();
            tileSizeValue.textContent = msg.tile_size.toString() + 'px';
        }
        updateHybridSlidersState();

        if (msg.keyframe_interval !== undefined && keyframeIntervalSelect) {
            keyframeIntervalSelect.value = msg.keyframe_interval.toString();
        }

        if (msg.bandwidth && bandwidthSelect) {
            bandwidthSelect.value = msg.bandwidth.toString();
            for (const radio of targetTypeRadios) {
                if (radio.value === 'bandwidth') radio.checked = true;
            }
        } else if (msg.quality && qualitySlider) {
            qualitySlider.value = msg.quality.toString();
            for (const radio of targetTypeRadios) {
                if (radio.value === 'quality') radio.checked = true;
            }
        }

        if (msg.framerate && framerateSelect) {
            framerateSelect.value = msg.framerate.toString();
        }

        if (msg.cpu_effort !== undefined && cpuEffortSlider) {
            cpuEffortSlider.value = msg.cpu_effort.toString();
        }

        if (msg.cpu_threads !== undefined && cpuThreadsSelect) {
            cpuThreadsSelect.value = msg.cpu_threads.toString();
        }

        if (msg.nvenc_latency !== undefined && nvencLatencyCheckbox) {
            nvencLatencyCheckbox.checked = msg.nvenc_latency;
        }

        if (msg.enable_desktop_mouse !== undefined && desktopMouseCheckbox) {
            desktopMouseCheckbox.checked = msg.enable_desktop_mouse;
        }

        if (msg.activity_hz !== undefined && activityHzSlider) {
            activityHzSlider.value = msg.activity_hz.toString();
            activityHzValue.textContent = msg.activity_hz.toString() + ' Hz';
        }

        if (msg.activity_timeout !== undefined && activityTimeoutSlider) {
            activityTimeoutSlider.value = msg.activity_timeout.toString();
            activityTimeoutValue.textContent = msg.activity_timeout.toString() + ' ms';
        }

        if (msg.enable_audio !== undefined && enableAudioCheckbox) {
            enableAudioCheckbox.checked = msg.enable_audio;
        }

        if (msg.audio_bitrate && audioBitrateSelect) {
            audioBitrateSelect.value = msg.audio_bitrate;
        }

        if (msg.hdpi !== undefined && hdpiSelect) {
            hdpiSelect.value = msg.hdpi.toString();
            currentHdpi = msg.hdpi;
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
        if (pendingHdpi !== null || pendingMaxRes !== null) {
            const h = pendingHdpi !== null ? pendingHdpi : parseInt(hdpiSelect.value, 10);
            const r = pendingMaxRes !== null ? pendingMaxRes : parseInt(maxResSelect.value, 10);
            pendingHdpi = null;
            pendingMaxRes = null;
            const updatedConfig = buildConfigMessage();
            updatedConfig.hdpi = h;
            updatedConfig.max_res = r;
            session.sendConfig(updatedConfig);
        }
    }

    if (msg.type === 'display_effect') {
        handleDisplayEffectMessage(msg, currentHdpi);
    }
}

wireConfigControls({
    sendConfig,
    scheduleResize: () => triggerResizeUpdate(),
    reinitDecoder: () => webcodecs.initDecoder(),
    setPendingHdpi: (val) => { pendingHdpi = val; },
    setPendingMaxRes: (val) => { pendingMaxRes = val; }
});

triggerResizeUpdate = () => {
    const width = (globalThis as any).innerWidth;
    const height = (globalThis as any).innerHeight;
    session.sendResize(width, height);
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
                session.sendInput(JSON.stringify({ type: 'clipboard', text }));
            }
        } catch (e) {
            // Ignore clipboard errors
        }
    });
}
