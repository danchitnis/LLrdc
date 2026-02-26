import { overlayEl } from './ui';

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
        overlayEl.addEventListener('mousemove', (e: MouseEvent) => {
            const now = Date.now();
            if (now - lastMove < 8) return;
            lastMove = now;
            const rect = overlayEl.getBoundingClientRect();
            if (rect.width === 0) return;
            const x = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
            const y = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height));
            sendMouse('mousemove', x, y, null);
        });

        overlayEl.addEventListener('mousedown', (e: MouseEvent) => {
            sendMouse('mousedown', null, null, e.button);
            e.preventDefault();
        });

        overlayEl.addEventListener('mouseup', (e: MouseEvent) => {
            sendMouse('mouseup', null, null, e.button);
            e.preventDefault();
        });

        overlayEl.addEventListener('contextmenu', (e: MouseEvent) => {
            e.preventDefault();
            return false;
        });
    }
}
