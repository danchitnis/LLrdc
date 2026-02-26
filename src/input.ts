import { overlayEl, videoEl, displayEl } from './ui';

export function setupInput(sendMsg: (data: string) => void) {
    window.addEventListener('keydown', (event: KeyboardEvent) => {
        if (['Space', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Tab', 'Backspace', 'Enter', 'Escape', 'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10', 'F11', 'F12'].includes(event.code) || event.key === ' ') {
            event.preventDefault();
        }
        sendMsg(JSON.stringify({ type: 'keydown', key: event.key }));
    });

    window.addEventListener('keyup', (event: KeyboardEvent) => {
        sendMsg(JSON.stringify({ type: 'keyup', key: event.key }));
    });

    const sendMouse = (type: string, x: number | null, y: number | null, button: number | null) => {
        sendMsg(JSON.stringify({ type, x, y, button }));
    };

    if (overlayEl) {
        let lastMove = 0;

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
            
            sendMouse('mousemove', pos.x, pos.y, null);
        });

        overlayEl.addEventListener('mousedown', (e: MouseEvent) => {
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
    }
}
