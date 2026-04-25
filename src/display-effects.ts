import { overlayEl, sharpnessCtx, sharpnessLayerEl } from './ui';

export function clearLosslessCanvas(x?: number, y?: number, w?: number, h?: number) {
    if (sharpnessCtx && sharpnessLayerEl) {
        if (x !== undefined && y !== undefined && w !== undefined && h !== undefined) {
            sharpnessCtx.clearRect(x, y, w, h);
        } else {
            sharpnessCtx.clearRect(0, 0, sharpnessLayerEl.width, sharpnessLayerEl.height);
        }
    }
}

export function handleDisplayEffectMessage(msg: Record<string, unknown>, currentHdpi: number): boolean {
    if (msg.type === 'lossless_patch') {
        if (sharpnessCtx && msg.data && typeof msg.data === 'string' && typeof msg.x === 'number' && typeof msg.y === 'number') {
            const ctx = sharpnessCtx;
            const img = new Image();
            img.onload = () => {
                ctx.drawImage(img, msg.x as number, msg.y as number);
            };
            img.src = msg.data;
        }
        return true;
    }

    if (msg.type === 'clear_lossless') {
        if (msg.rects && Array.isArray(msg.rects)) {
            for (const rect of msg.rects) {
                clearLosslessCanvas(rect.x as number, rect.y as number, rect.w as number, rect.h as number);
            }
        } else {
            clearLosslessCanvas(msg.x as number | undefined, msg.y as number | undefined, msg.w as number | undefined, msg.h as number | undefined);
        }
        return true;
    }

    if (msg.type !== 'cursor_shape') {
        return false;
    }

    if (overlayEl && typeof msg.dataURL === 'string' && typeof msg.xhot === 'number' && typeof msg.yhot === 'number') {
        const dataURL = msg.dataURL;
        const xhot = msg.xhot;
        const yhot = msg.yhot;
        const img = new Image();
        img.onload = () => {
            const hdpiScale = currentHdpi / 100;
            const baseWidth = img.width / hdpiScale;
            const baseHeight = img.height / hdpiScale;

            const minSize = 24;
            let scale = 1 / hdpiScale;

            if (baseWidth > 0 && baseHeight > 0 && (baseWidth < minSize || baseHeight < minSize)) {
                const minScale = Math.max(minSize / baseWidth, minSize / baseHeight);
                scale = scale * minScale;
            }

            if (img.width > 0 && img.height > 0 && Math.abs(scale - 1.0) > 0.01) {
                const newWidth = Math.round(img.width * scale);
                const newHeight = Math.round(img.height * scale);
                const newXhot = Math.round(xhot * scale);
                const newYhot = Math.round(yhot * scale);

                const canvas = document.createElement('canvas');
                canvas.width = newWidth;
                canvas.height = newHeight;
                const ctx = canvas.getContext('2d');
                if (ctx) {
                    ctx.imageSmoothingEnabled = true;
                    ctx.drawImage(img, 0, 0, newWidth, newHeight);
                    overlayEl.style.cursor = `url(${canvas.toDataURL('image/png')}) ${newXhot} ${newYhot}, auto`;
                    return;
                }
            }
            overlayEl.style.cursor = `url(${dataURL}) ${xhot} ${yhot}, auto`;
        };
        img.src = dataURL;
    }

    return true;
}
