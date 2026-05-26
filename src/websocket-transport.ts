import { log } from './ui';

export class WebSocketTransport {
    private ws: WebSocket | null = null;
    private onBinaryMessage: (buffer: ArrayBuffer) => void;
    private onJsonMessage: (msg: Record<string, unknown>) => void;
    public isActive = false;
    public isConnecting = false;
    public totalBytesReceived = 0;
    public fps = 0;
    private frameCount = 0;
    private lastFpsUpdate = Date.now();
    
    constructor(onBinaryMessage: (buffer: ArrayBuffer) => void, onJsonMessage: (msg: Record<string, unknown>) => void) {
        this.onBinaryMessage = onBinaryMessage;
        this.onJsonMessage = onJsonMessage;
        setInterval(() => {
            const now = Date.now();
            const elapsed = (now - this.lastFpsUpdate) / 1000;
            this.fps = Math.round(this.frameCount / elapsed);
            this.frameCount = 0;
            this.lastFpsUpdate = now;
        }, 1000);
    }

    public connect(url: string) {
        if (this.isActive || this.isConnecting) return;
        this.isConnecting = true;

        log(`[WebSocket] Connecting to ${url}...`);
        
        try {
            this.ws = new WebSocket(url);
            this.ws.binaryType = 'arraybuffer';

            this.ws.onopen = () => {
                log('[WebSocket] Connected successfully');
                this.isActive = true;
                this.isConnecting = false;
            };

            this.ws.onmessage = (event) => {
                if (event.data instanceof ArrayBuffer) {
                    this.handleBinaryMessage(event.data);
                } else if (typeof event.data === 'string') {
                    try {
                        const msg = JSON.parse(event.data);
                        this.onJsonMessage(msg);
                    } catch (e) {
                        log(`[WebSocket] JSON parse error: ${(e as Error).message}`);
                    }
                }
            };

            this.ws.onclose = () => {
                log('[WebSocket] Connection closed');
                this.cleanup();
            };

            this.ws.onerror = (e) => {
                log(`[WebSocket] Error: ${e}`);
                this.isConnecting = false;
            };

        } catch (e) {
            log(`[WebSocket] Connection FAILED: ${(e as Error).message}`);
            this.isActive = false;
            this.isConnecting = false;
        }
    }

    private cleanup() {
        this.isActive = false;
        this.isConnecting = false;
        this.ws = null;
    }

    private handleBinaryMessage(data: ArrayBuffer) {
        // WebSocket delivers full packets, so we don't need the framing logic that WebTransport (stream-based) needs
        // EXCEPT that the server is still prepending the 4-byte length and 1-byte type etc.
        // Wait, does the server WriteMessage(BinaryMessage, packet)? 
        // Yes: packet := append(header, frame...)
        // where header is 13 bytes: 4 (len) + 1 (type) + 8 (timestamp)
        
        // We should strip the 4-byte length because WebSocket already gives us the frame boundaries.
        // Actually, for consistency with onBinaryMessage (which expects the 9-byte header: type + timestamp), 
        // we should slice off the first 4 bytes.
        
        if (data.byteLength < 4) return;
        
        const dv = new DataView(data);
        const packetLen = dv.getUint32(0, false);
        
        if (data.byteLength >= 4 + packetLen) {
            this.totalBytesReceived += data.byteLength;
            this.frameCount++;
            // Slice off the 4-byte length, keeping the type (1 byte) and timestamp (8 bytes) and frame data
            const packet = data.slice(4, 4 + packetLen);
            this.onBinaryMessage(packet);
        }
    }

    public sendMsg(data: string) {
        if (!this.ws || !this.isActive) return;
        try {
            this.ws.send(data);
        } catch (e) {
            log(`[WebSocket] Failed to send message: ${(e as Error).message}`);
        }
    }
}
