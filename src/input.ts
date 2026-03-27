import { overlayEl, videoEl, displayEl } from './ui';

export function setupInput(sendMsg: (data: string) => void) {
    if (!overlayEl) return;

    let pointerOverCanvas = false;
    overlayEl.addEventListener('mouseenter', () => { pointerOverCanvas = true; });
    overlayEl.addEventListener('mouseleave', () => { pointerOverCanvas = false; });

    const getNormalizedPos = (e: MouseEvent): { x: number, y: number } | null => {
        const rect = overlayEl.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return null;

        let videoW = videoEl?.videoWidth || displayEl?.width || 1920;
        let videoH = videoEl?.videoHeight || displayEl?.height || 1080;

        const containerRatio = rect.width / rect.height;
        const videoRatio = videoW / videoH;
        
        let drawW = rect.width;
        let drawH = rect.height;
        let drawX = 0;
        let drawY = 0;

        if (containerRatio > videoRatio) {
            drawW = rect.height * videoRatio;
            drawX = (rect.width - drawW) / 2;
        } else {
            drawH = rect.width / videoRatio;
            drawY = (rect.height - drawH) / 2;
        }

        const mouseX = e.clientX - rect.left;
        const mouseY = e.clientY - rect.top;

        const nx = (mouseX - drawX) / drawW;
        const ny = (mouseY - drawY) / drawH;

        return {
            x: Math.max(0, Math.min(1, nx)),
            y: Math.max(0, Math.min(1, ny))
        };
    };

    let pendingMousePos: { x: number, y: number } | null = null;
    let isMouseUpdatePending = false;

    overlayEl.addEventListener('mousemove', (e: MouseEvent) => {
        const pos = getNormalizedPos(e);
        if (!pos) return;

        pendingMousePos = pos;

        if (!isMouseUpdatePending) {
            isMouseUpdatePending = true;
            requestAnimationFrame(() => {
                if (pendingMousePos) {
                    sendMsg(JSON.stringify({ type: 'mousemove', x: pendingMousePos.x, y: pendingMousePos.y }));
                    pendingMousePos = null;
                }
                isMouseUpdatePending = false;
            });
        }
    });

    overlayEl.tabIndex = 0;
    overlayEl.style.outline = 'none';

    overlayEl.addEventListener('mousedown', (e: MouseEvent) => {
        overlayEl.focus();
        const pos = getNormalizedPos(e);
        if (pos) {
            sendMsg(JSON.stringify({ type: 'mousemove', x: pos.x, y: pos.y }));
        }
        sendMsg(JSON.stringify({ type: 'mousebtn', button: e.button, action: 'mousedown' }));
        e.preventDefault();
    });

    overlayEl.addEventListener('mouseup', (e: MouseEvent) => {
        sendMsg(JSON.stringify({ type: 'mousebtn', button: e.button, action: 'mouseup' }));
        e.preventDefault();
    });

    overlayEl.addEventListener('keydown', (e: KeyboardEvent) => {
        sendMsg(JSON.stringify({ type: 'keydown', key: e.code }));
        e.preventDefault();
    });

    overlayEl.addEventListener('keyup', (e: KeyboardEvent) => {
        sendMsg(JSON.stringify({ type: 'keyup', key: e.code }));
        e.preventDefault();
    });

    overlayEl.addEventListener('wheel', (e: WheelEvent) => {
        sendMsg(JSON.stringify({ type: 'wheel', deltaX: e.deltaX, deltaY: e.deltaY }));
        e.preventDefault();
    }, { passive: false });

    overlayEl.addEventListener('contextmenu', (e: MouseEvent) => {
        e.preventDefault();
        return false;
    });
}
