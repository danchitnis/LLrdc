import { overlayEl, displayContainerEl, displayEl, videoEl } from './ui';

export function setupInput(sendMsg: (data: string) => void) {
    if (!overlayEl) return;

    const focusOverlay = () => {
        if (document.activeElement !== overlayEl) {
            overlayEl.focus({ preventScroll: true });
        }
    };

    let pressedButton: number | null = null;

    const isWithinDisplayContainer = (e: MouseEvent): boolean => {
        if (!displayContainerEl) return false;
        const rect = displayContainerEl.getBoundingClientRect();
        return e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom;
    };

    const getNormalizedPos = (e: MouseEvent): { x: number, y: number } | null => {
        // Determine the active display element (canvas for WebCodecs, video for WebRTC)
        let activeEl: HTMLElement = displayEl;
        let internalW = displayEl.width;
        let internalH = displayEl.height;

        // If WebRTC mode is active, use the video element
        if (videoEl && videoEl.style.display !== 'none' && videoEl.videoWidth > 0) {
            activeEl = videoEl;
            internalW = videoEl.videoWidth;
            internalH = videoEl.videoHeight;
        }

        if (!activeEl) return null;
        const rect = activeEl.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return null;

        const videoRatio = internalW / internalH;

        // The element dimensions (e.g. 1280x768 container)
        const containerW = rect.width;
        const containerH = rect.height;
        const containerRatio = containerW / containerH;

        let drawW = containerW;
        let drawH = containerH;
        let drawX = 0;
        let drawY = 0;

        // Browser's "object-fit: contain" logic:
        if (containerRatio > videoRatio) {
            // Pillarboxed (bars on left/right)
            drawW = containerH * videoRatio;
            drawX = (containerW - drawW) / 2;
        } else {
            // Letterboxed (bars on top/bottom)
            drawH = containerW / videoRatio;
            drawY = (containerH - drawH) / 2;
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

    let isMouseMovePending = false;
    let lastMouseMoveEvent: MouseEvent | null = null;

    const processMouseMove = () => {
        if (!lastMouseMoveEvent) {
            isMouseMovePending = false;
            return;
        }

        const e = lastMouseMoveEvent;
        lastMouseMoveEvent = null;

        const withinDisplay = isWithinDisplayContainer(e);
        if (!withinDisplay && pressedButton === null) {
            isMouseMovePending = false;
            return;
        }

        if (withinDisplay) {
            focusOverlay();
        }

        const pos = getNormalizedPos(e);
        if (pos) {
            sendMsg(JSON.stringify({ type: 'mousemove', x: pos.x, y: pos.y, ts: Date.now() }));
        }

        isMouseMovePending = false;
    };

    const forwardMouseMove = (e: MouseEvent) => {
        lastMouseMoveEvent = e;
        if (!isMouseMovePending) {
            isMouseMovePending = true;
            requestAnimationFrame(processMouseMove);
        }
    };

    overlayEl.addEventListener('mouseenter', () => {
        focusOverlay();
    });
    window.addEventListener('mousemove', forwardMouseMove, true);

    overlayEl.tabIndex = 0;
    overlayEl.style.outline = 'none';
    overlayEl.setAttribute('aria-label', 'Remote desktop input overlay');

    overlayEl.addEventListener('mousedown', (e: MouseEvent) => {
        pressedButton = e.button;
        focusOverlay();
        const pos = getNormalizedPos(e);
        if (pos) {
            sendMsg(JSON.stringify({ type: 'mousemove', x: pos.x, y: pos.y, ts: Date.now() }));
        }
        sendMsg(JSON.stringify({ type: 'mousebtn', button: e.button, action: 'mousedown', ts: Date.now() }));
        e.preventDefault();
    });

    window.addEventListener('mouseup', (e: MouseEvent) => {
        if (pressedButton === null && !isWithinDisplayContainer(e)) {
            return;
        }
        pressedButton = null;
        if (isWithinDisplayContainer(e)) {
            const pos = getNormalizedPos(e);
            if (pos) {
                sendMsg(JSON.stringify({ type: 'mousemove', x: pos.x, y: pos.y, ts: Date.now() }));
            }
        }
        sendMsg(JSON.stringify({ type: 'mousebtn', button: e.button, action: 'mouseup', ts: Date.now() }));
        e.preventDefault();
    }, true);

    overlayEl.addEventListener('keydown', (e: KeyboardEvent) => {
        sendMsg(JSON.stringify({ type: 'keydown', key: e.code, ts: Date.now() }));
        e.preventDefault();
    });

    overlayEl.addEventListener('keyup', (e: KeyboardEvent) => {
        sendMsg(JSON.stringify({ type: 'keyup', key: e.code, ts: Date.now() }));
        e.preventDefault();
    });

    overlayEl.addEventListener('wheel', (e: WheelEvent) => {
        sendMsg(JSON.stringify({ type: 'wheel', deltaX: e.deltaX, deltaY: e.deltaY, ts: Date.now() }));
        e.preventDefault();
    }, { passive: false });

    overlayEl.addEventListener('contextmenu', (e: MouseEvent) => {
        e.preventDefault();
        return false;
    });

    window.addEventListener('mousedown', (e: MouseEvent) => {
        if (displayContainerEl?.contains(e.target as Node)) {
            focusOverlay();
        }
    });
}
