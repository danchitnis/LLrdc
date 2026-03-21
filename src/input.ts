import { overlayEl, videoEl, displayEl, clipboardArea } from './ui';

export let pendingClipboard: string | null = null;
export function setPendingClipboard(text: string) {
    pendingClipboard = text;
}

let clipboardEnabled = true;
export function setClipboardEnabled(enabled: boolean) {
    clipboardEnabled = enabled;
    console.log(`>>> [Input] Clipboard ${enabled ? 'enabled' : 'disabled'}`);
}

function processPendingClipboard() {
    if (!clipboardEnabled) return;
    if (pendingClipboard !== null) {
        if (clipboardArea) {
            clipboardArea.value = pendingClipboard;
            clipboardArea.select();
            try {
                document.execCommand('copy');
            } catch {
                // ignore
            }
        }
        
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(pendingClipboard).catch(() => {});
        }
        pendingClipboard = null;
    }
}

export function setupInput(sendMsg: (data: string) => void, onMouseMoveLocal?: () => void) {
    let withheldKey: string | null = null;
    const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;

    const sentModifiers = {
        Control: false,
        Shift: false,
        Alt: false,
        Meta: false
    };

    const sendMsgWrapped = (msgObj: { type: string; key?: string; text?: string; x?: number | null; y?: number | null; button?: number | null; paste?: boolean }) => {
        if (msgObj.type === 'keydown' || msgObj.type === 'keyup') {
            const isDown = msgObj.type === 'keydown';
            if (msgObj.key === 'Control') sentModifiers.Control = isDown;
            if (msgObj.key === 'Shift') sentModifiers.Shift = isDown;
            if (msgObj.key === 'Alt') sentModifiers.Alt = isDown;
            if (msgObj.key === 'Meta') sentModifiers.Meta = isDown;
        }
        sendMsg(JSON.stringify(msgObj));
    };

    let pointerOverCanvas = false;

    const focusClipboard = () => {
        // Only steal focus when clipboard is enabled AND pointer is on the canvas
        if (!clipboardEnabled || !pointerOverCanvas) {
            return;
        }
        if (clipboardArea) {
            clipboardArea.focus({ preventScroll: true });
        }
    };

    // Periodically focus the textarea for clipboard capture
    setInterval(focusClipboard, 1000);

    const syncModifiers = (event: KeyboardEvent | MouseEvent) => {
        const check = (isPressed: boolean, name: keyof typeof sentModifiers) => {
            if (!isPressed && sentModifiers[name]) {
                console.log(`>>> [Input] Modifier Sync: Auto-releasing ${name}`);
                sendMsgWrapped({ type: 'keyup', key: name });
            }
        };

        // On Mac, we map Meta to Control on the remote side
        const remoteCtrl = event.ctrlKey || (isMac && event.metaKey);
        check(remoteCtrl, 'Control');
        check(event.shiftKey, 'Shift');
        check(event.altKey, 'Alt');
        // If not Mac, Meta is just Meta (Super)
        if (!isMac) {
            check(event.metaKey, 'Meta');
        }
    };

    const releaseModifiers = () => {
        console.log('>>> [Input] Guard: Releasing all modifiers');
        sendMsgWrapped({ type: 'keyup', key: 'Control' });
        sendMsgWrapped({ type: 'keyup', key: 'Shift' });
        sendMsgWrapped({ type: 'keyup', key: 'Alt' });
        sendMsgWrapped({ type: 'keyup', key: 'Meta' });
    };

    window.addEventListener('blur', () => {
        withheldKey = null;
        releaseModifiers();
    });

    window.addEventListener('mousedown', (event: MouseEvent) => {
        // Modifier sync on click to prevent stuck keys from tab-switching
        syncModifiers(event);
        focusClipboard();
    });

    window.addEventListener('keydown', (event: KeyboardEvent) => {
        syncModifiers(event);
        
        const isV = event.key.toLowerCase() === 'v' || event.code === 'KeyV';
        const isC = event.key.toLowerCase() === 'c' || event.code === 'KeyC';
        const isA = event.key.toLowerCase() === 'a' || event.code === 'KeyA';
        const hasMod = event.ctrlKey || event.metaKey;

        // Habit translation for Mac users (Cmd -> Ctrl) and standard Ctrl support
        if (hasMod && isV) {
            console.log('>>> [Input] Intercepted Paste shortcut');
            withheldKey = event.key;
            focusClipboard();
            return;
        }
        // Process pending clipboard AFTER paste check so we don't overwrite
        // the host clipboard right before a paste operation
        processPendingClipboard();
        if (hasMod && isC) {
            console.log('>>> [Input] Intercepted Copy shortcut');
            sendMsgWrapped({ type: 'key', key: 'ctrl+c' });
            return;
        }
        if (hasMod && isA) {
            console.log('>>> [Input] Intercepted Select-All shortcut');
            sendMsgWrapped({ type: 'key', key: 'ctrl+a' });
            return;
        }

        if (['Space', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Tab', 'Backspace', 'Enter', 'Escape', 'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10', 'F11', 'F12'].includes(event.code) || event.key === ' ') {
            event.preventDefault();
        }

        let key = event.key;
        // General mapping for other shortcuts (Cmd+S, etc)
        // Check both 'Meta' key name and 'OS' (older browsers)
        if (isMac && (key === 'Meta' || key === 'OS')) {
            key = 'Control';
        }

        sendMsgWrapped({ type: 'keydown', key });
    });

    window.addEventListener('keyup', (event: KeyboardEvent) => {
        if (withheldKey && (withheldKey.toLowerCase() === event.key.toLowerCase() || withheldKey === event.code)) {
            withheldKey = null;
            return;
        }

        let key = event.key;
        if (isMac && (key === 'Meta' || key === 'OS')) {
            key = 'Control';
        }
        sendMsgWrapped({ type: 'keyup', key });
    });

    if (clipboardArea) {
        clipboardArea.addEventListener('paste', (event: ClipboardEvent) => {
            if (!clipboardEnabled) return;
            const text = event.clipboardData?.getData('text');
            console.log(`>>> [Input] Browser Paste Event: ${text?.length || 0} chars`);
            if (text) {
                sendMsgWrapped({ type: 'clipboard_set', text, paste: true });
            }
            withheldKey = null;
            if (clipboardArea) clipboardArea.value = '';
        });
    }

    const sendMouse = (type: string, x: number | null, y: number | null, button: number | null) => {
        sendMsg(JSON.stringify({ type, x, y, button }));
    };

    if (overlayEl) {
        let lastMove = 0;

        overlayEl.addEventListener('mouseenter', () => { pointerOverCanvas = true; });
        overlayEl.addEventListener('mouseleave', () => { pointerOverCanvas = false; });

        const getNormalizedPos = (e: MouseEvent): { x: number, y: number } | null => {
            const rect = overlayEl.getBoundingClientRect();
            if (rect.width === 0 || rect.height === 0) return null;

            // Determine intrinsic video size
            let videoW = 0;
            let videoH = 0;
            if (videoEl && videoEl.videoWidth > 0 && videoEl.videoHeight > 0) {
                videoW = videoEl.videoWidth;
                videoH = videoEl.videoHeight;
            } else if (displayEl && displayEl.width > 0 && displayEl.height > 0) {
                videoW = displayEl.width;
                videoH = displayEl.height;
            }

            if (videoW === 0 || videoH === 0) {
                // Fallback to full container if no video size is available yet
                return {
                    x: Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
                    y: Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height))
                };
            }

            // Calculate letterboxing bounds for object-fit: contain
            const containerRatio = rect.width / rect.height;
            const videoRatio = videoW / videoH;
            
            let drawW = rect.width;
            let drawH = rect.height;
            let drawX = 0;
            let drawY = 0;

            if (containerRatio > videoRatio) {
                // Letterboxed on left/right
                drawW = rect.height * videoRatio;
                drawX = (rect.width - drawW) / 2;
            } else {
                // Letterboxed on top/bottom
                drawH = rect.width / videoRatio;
                drawY = (rect.height - drawH) / 2;
            }

            const mouseX = e.clientX - rect.left;
            const mouseY = e.clientY - rect.top;

            // Normalize coordinate within the drawn area
            const nx = (mouseX - drawX) / drawW;
            const ny = (mouseY - drawY) / drawH;

            // Clamp to 0-1 so we don't send coordinates outside the desktop if clicking black bars
            return {
                x: Math.max(0, Math.min(1, nx)),
                y: Math.max(0, Math.min(1, ny))
            };
        };

        overlayEl.addEventListener('mousemove', (e: MouseEvent) => {
            const now = Date.now();
            if (now - lastMove < 8) return;
            lastMove = now;

            const pos = getNormalizedPos(e);
            if (!pos) return;

            if (onMouseMoveLocal) onMouseMoveLocal();
            sendMouse('mousemove', pos.x, pos.y, null);
        });
        overlayEl.addEventListener('mousedown', (e: MouseEvent) => {
            processPendingClipboard();
            focusClipboard();
            const pos = getNormalizedPos(e);
            if (pos) {
                // Optional: Update position right before click
                sendMouse('mousemove', pos.x, pos.y, null);
            }
            sendMouse('mousedown', null, null, e.button);
            e.preventDefault();
        });

        overlayEl.addEventListener('mouseup', (e: MouseEvent) => {
            const pos = getNormalizedPos(e);
            if (pos) {
                sendMouse('mousemove', pos.x, pos.y, null);
            }
            sendMouse('mouseup', null, null, e.button);
            e.preventDefault();
        });

        overlayEl.addEventListener('contextmenu', (e: MouseEvent) => {
            e.preventDefault();
            return false;
        });

        let wheelAccumX = 0;
        let wheelAccumY = 0;
        overlayEl.addEventListener('wheel', (e: WheelEvent) => {
            let dx = e.deltaX;
            let dy = e.deltaY;
            
            // Normalize scroll modes
            if (e.deltaMode === 1) { // DOM_DELTA_LINE
                dx *= 33;
                dy *= 33;
            } else if (e.deltaMode === 2) { // DOM_DELTA_PAGE
                dx *= 800;
                dy *= 800;
            }

            wheelAccumX += dx;
            wheelAccumY += dy;

            const THRESHOLD = 20; // Lower threshold for smoother scrolling
            let sendDx = 0;
            let sendDy = 0;

            if (Math.abs(wheelAccumX) >= THRESHOLD) {
                sendDx = Math.sign(wheelAccumX) * Math.floor(Math.abs(wheelAccumX) / THRESHOLD);
                wheelAccumX -= sendDx * THRESHOLD;
            }
            if (Math.abs(wheelAccumY) >= THRESHOLD) {
                sendDy = Math.sign(wheelAccumY) * Math.floor(Math.abs(wheelAccumY) / THRESHOLD);
                wheelAccumY -= sendDy * THRESHOLD;
            }

            if (sendDx !== 0 || sendDy !== 0) {
                sendMsg(JSON.stringify({ type: 'wheel', deltaX: sendDx, deltaY: sendDy }));
            }
            e.preventDefault();
        }, { passive: false });
    }
}

