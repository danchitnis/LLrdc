import { directBufferStatusEl, videoCodecSelect, setDirectBufferActive } from './ui';

export function updateDirectBufferUi(msg: Record<string, unknown>) {
    const captureMode = typeof msg.captureMode === 'string' ? msg.captureMode : 'compat';
    const directRequested = msg.directBufferRequested === true;
    const directSupported = msg.directBufferSupported === true;
    const directActive = msg.directBufferActive === true;
    const directReason = typeof msg.directBufferReason === 'string' ? msg.directBufferReason : '';

    setDirectBufferActive(directActive);

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

    if (videoCodecSelect && !msg.capabilities) {
        Array.from(videoCodecSelect.options).forEach(option => {
            if (captureMode === 'direct') {
                const isHardware = option.value.includes('_nvenc') || option.value.includes('_vaapi');
                option.disabled = !isHardware;
            } else {
                option.disabled = false;
            }
        });
    }

}
