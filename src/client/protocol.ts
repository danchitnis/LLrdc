export interface BinaryVideoPacket {
    type: number;
    timestampMs: number;
    chunkData: Uint8Array;
}

export function normalizeCodecFamily(codec: string): string {
    if (!codec) return 'vp8';
    if (codec.startsWith('h264')) return 'h264';
    if (codec.startsWith('h265')) return 'h265';
    if (codec.startsWith('av1')) return 'av1';
    return codec;
}

export function parseBinaryVideoPacket(buffer: ArrayBuffer): BinaryVideoPacket | null {
    if (buffer.byteLength < 9) {
        return null;
    }

    const dv = new DataView(buffer);
    const type = dv.getUint8(0);
    if (type !== 1) {
        return null;
    }

    return {
        type,
        timestampMs: dv.getFloat64(1, false),
        chunkData: new Uint8Array(buffer, 9),
    };
}

export function detectKeyFrame(codec: string, chunkData: Uint8Array): boolean {
    if (codec.startsWith('h264')) {
        for (let i = 0; i < chunkData.length - 4; i++) {
            if (chunkData[i] === 0 && chunkData[i + 1] === 0 && chunkData[i + 2] === 0 && chunkData[i + 3] === 1) {
                const nalType = chunkData[i + 4] & 0x1F;
                if (nalType === 5 || nalType === 7) {
                    return true;
                }
            }
        }
        return false;
    }

    if (codec.startsWith('h265')) {
        for (let i = 0; i < chunkData.length - 4; i++) {
            if (chunkData[i] === 0 && chunkData[i + 1] === 0 && chunkData[i + 2] === 0 && chunkData[i + 3] === 1) {
                const nalType = (chunkData[i + 4] & 0x7E) >> 1;
                if (nalType === 19 || nalType === 20 || nalType === 21 || nalType === 32 || nalType === 33 || nalType === 34) {
                    return true;
                }
            }
        }
        return false;
    }

    if (codec.startsWith('av1')) {
        let pos = 0;
        while (pos < chunkData.length && pos < 100) {
            const obuType = (chunkData[pos] >> 3) & 0x0F;
            if (obuType === 1) {
                return true;
            }
            if (obuType === 2) {
                pos += 2;
                continue;
            }
            break;
        }
        return false;
    }

    return (chunkData[0] & 0x01) === 0;
}
