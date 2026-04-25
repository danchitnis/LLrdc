import { chromaCheckbox, directBufferStatusEl, videoCodecSelect } from './ui';

export function updateDirectBufferUi(msg: Record<string, unknown>) {
    const captureMode = typeof msg.captureMode === 'string' ? msg.captureMode : 'compat';
    const directRequested = msg.directBufferRequested === true;
    const directSupported = msg.directBufferSupported === true;
    const directActive = msg.directBufferActive === true;
    const directReason = typeof msg.directBufferReason === 'string' ? msg.directBufferReason : '';

    if (directBufferStatusEl) {
        if (!directRequested || captureMode !== 'direct') {
            directBufferStatusEl.textContent = 'Compat mode';
        } else if (directActive) {
            directBufferStatusEl.textContent = 'Active';
        } else if (directSupported) {
            directBufferStatusEl.textContent = 'Supported, waiting for hardware capture';
        } else {
            directBufferStatusEl.textContent = 'Unavailable';
        }
        directBufferStatusEl.title = directReason || 'Read-only startup status for DMA-BUF direct capture';
    }

    if (videoCodecSelect) {
        Array.from(videoCodecSelect.options).forEach(option => {
            if (captureMode === 'direct') {
                const isHardware = option.value.endsWith('_nvenc') || option.value.endsWith('_qsv') || option.value.endsWith('_vaapi');
                option.disabled = !isHardware;
            } else {
                option.disabled = false;
            }
        });
    }

    if (chromaCheckbox) {
        if (captureMode === 'direct') {
            chromaCheckbox.checked = false;
            chromaCheckbox.disabled = true;
            if (chromaCheckbox.parentElement) {
                chromaCheckbox.parentElement.style.opacity = '0.5';
                chromaCheckbox.parentElement.title = 'Direct capture mode currently requires YUV 4:2:0';
            }
        } else if (chromaCheckbox.parentElement) {
            chromaCheckbox.parentElement.style.opacity = '1';
            chromaCheckbox.parentElement.title = 'Improve text clarity by avoiding chroma subsampling (H.264/H.265/AV1 only)';
        }
    }
}
