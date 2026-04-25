import {
    audioBitrateSelect,
    bandwidthSelect,
    chromaCheckbox,
    clientGpuCheckbox,
    configBtn,
    configDropdown,
    configTabBtns,
    cpuEffortSlider,
    cpuEffortValue,
    cpuThreadsSelect,
    desktopMouseCheckbox,
    damageTrackingCheckbox,
    enableAudioCheckbox,
    framerateSelect,
    hdpiSelect,
    hybridCheckbox,
    keyframeIntervalSelect,
    maxResSelect,
    mpdecimateCheckbox,
    nvencLatencyCheckbox,
    qualitySlider,
    qualityValue,
    settleSlider,
    settleValue,
    targetTypeRadios,
    tileSizeSlider,
    tileSizeValue,
    vbrCheckbox,
    vbrThresholdGroup,
    vbrThresholdSlider,
    vbrThresholdValue,
    videoCodecSelect,
    webrtcBufferSlider,
    webrtcBufferValue,
    webrtcLowLatencyCheckbox,
    activityHzSlider,
    activityHzValue,
    activityTimeoutSlider,
    activityTimeoutValue,
    clipboardCheckbox,
} from './ui';

export function updateHybridSlidersState() {
    if (hybridCheckbox) {
        if (settleSlider) settleSlider.disabled = !hybridCheckbox.checked;
        if (tileSizeSlider) tileSizeSlider.disabled = !hybridCheckbox.checked;
    }
}

interface ConfigControlHandlers {
    sendConfig: () => void;
    scheduleResize: () => void;
    reinitDecoder: () => void;
    setPendingHdpi: (value: number) => void;
    setPendingMaxRes: (value: number) => void;
}

export function wireConfigControls(handlers: ConfigControlHandlers) {
    const { sendConfig, scheduleResize, reinitDecoder, setPendingHdpi, setPendingMaxRes } = handlers;

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

    wireSelect(bandwidthSelect, sendConfig);
    if (damageTrackingCheckbox) damageTrackingCheckbox.addEventListener('change', sendConfig);
    if (mpdecimateCheckbox) mpdecimateCheckbox.addEventListener('change', sendConfig);
    if (chromaCheckbox) chromaCheckbox.addEventListener('change', sendConfig);
    wireSelect(keyframeIntervalSelect, sendConfig);
    wireSelect(framerateSelect, sendConfig);
    wireSelect(cpuThreadsSelect, sendConfig);
    if (nvencLatencyCheckbox) nvencLatencyCheckbox.addEventListener('change', sendConfig);
    if (webrtcLowLatencyCheckbox) webrtcLowLatencyCheckbox.addEventListener('change', sendConfig);
    if (desktopMouseCheckbox) desktopMouseCheckbox.addEventListener('change', sendConfig);
    if (enableAudioCheckbox) enableAudioCheckbox.addEventListener('change', sendConfig);
    wireSelect(audioBitrateSelect, sendConfig);

    if (vbrCheckbox) {
        vbrCheckbox.addEventListener('change', () => {
            if (vbrThresholdGroup) vbrThresholdGroup.style.display = vbrCheckbox.checked ? 'flex' : 'none';
            sendConfig();
        });
    }

    if (hybridCheckbox) {
        hybridCheckbox.addEventListener('change', () => {
            updateHybridSlidersState();
            sendConfig();
        });
        updateHybridSlidersState();
    }

    wireSlider(vbrThresholdSlider, vbrThresholdValue, sendConfig);
    wireSlider(settleSlider, settleValue, sendConfig);
    wireSlider(tileSizeSlider, tileSizeValue, sendConfig);
    wireSlider(qualitySlider, qualityValue, sendConfig);
    wireSlider(cpuEffortSlider, cpuEffortValue, sendConfig);
    wireSlider(webrtcBufferSlider, webrtcBufferValue, sendConfig);
    wireSlider(activityHzSlider, activityHzValue, sendConfig);
    wireSlider(activityTimeoutSlider, activityTimeoutValue, sendConfig);

    if (hdpiSelect) {
        hdpiSelect.addEventListener('change', () => {
            setPendingHdpi(parseInt(hdpiSelect.value, 10));
            sendConfig();
        });
    }

    if (maxResSelect) {
        maxResSelect.addEventListener('change', () => {
            setPendingMaxRes(parseInt(maxResSelect.value, 10));
            sendConfig();
            scheduleResize();
        });
    }

    if (configTabBtns) {
        configTabBtns.forEach(btn => {
            btn.addEventListener('click', () => {
                configTabBtns.forEach(b => b.classList.remove('active'));
                document.querySelectorAll('.config-tab-content').forEach(c => {
                    (c as HTMLElement).style.display = 'none';
                    c.classList.remove('active');
                });

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

    if (clipboardCheckbox) {
        clipboardCheckbox.addEventListener('change', () => {});
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
            reinitDecoder();
            sendConfig();
        });
    }
}

function wireSlider(slider: HTMLInputElement | null, valueEl: HTMLElement | null, sendConfig: () => void) {
    if (!slider || !valueEl) return;
    slider.addEventListener('input', (e) => {
        valueEl.textContent = (e.target as HTMLInputElement).value;
    });
    slider.addEventListener('change', sendConfig);
}

function wireSelect(select: HTMLSelectElement | null, sendConfig: () => void) {
    if (!select) return;
    select.addEventListener('input', sendConfig);
    select.addEventListener('change', sendConfig);
}
