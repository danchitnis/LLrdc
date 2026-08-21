import { log } from './ui';

interface WebTransportStream {
    readable: ReadableStream<Uint8Array>;
    writable: WritableStream<Uint8Array>;
}

interface WebTransportConnection {
    ready: Promise<void>;
    closed: Promise<void>;
    createBidirectionalStream: () => Promise<WebTransportStream>;
    incomingUnidirectionalStreams: ReadableStream<ReadableStream<Uint8Array>>;
}

interface WebTransportOptions {
    serverCertificateHashes: Array<{ algorithm: 'sha-256'; value: Uint8Array }>;
}

interface WebTransportConstructor {
    new (url: string, options: WebTransportOptions): WebTransportConnection;
}

export class WebTransportManager {
    private transport: WebTransportConnection | null = null;
    private controlStream: WebTransportStream | null = null;
    private controlWriter: WritableStreamDefaultWriter | null = null;
    private onBinaryMessage: (buffer: ArrayBuffer) => void;
    private onJsonMessage: (msg: Record<string, unknown>) => void;
    private onConnected: () => void;
    public isWebTransportActive = false;
    public isConnecting = false;
    public totalBytesReceived = 0;
    public fps = 0;
    private frameCount = 0;
    private lastFpsUpdate = Date.now();
    
    constructor(
        onBinaryMessage: (buffer: ArrayBuffer) => void, 
        onJsonMessage: (msg: Record<string, unknown>) => void,
        onConnected: () => void
    ) {
        this.onBinaryMessage = onBinaryMessage;
        this.onJsonMessage = onJsonMessage;
        this.onConnected = onConnected;
        setInterval(() => {
            const now = Date.now();
            const elapsed = (now - this.lastFpsUpdate) / 1000;
            this.fps = Math.round(this.frameCount / elapsed);
            this.frameCount = 0;
            this.lastFpsUpdate = now;
        }, 1000);
    }

    public async connect(url: string, fingerprint: string) {
        if (this.isWebTransportActive || this.isConnecting) return;
        this.isConnecting = true;

        const wtGlobal = (globalThis as typeof globalThis & { WebTransport?: WebTransportConstructor }).WebTransport;
        if (!wtGlobal) {
            log('[WebTransport] Error: API not supported.');
            this.isConnecting = false;
            return;
        }

        try {
            log(`[WebTransport] Connecting to ${url}...`);
            
            // Convert base64 fingerprint to Uint8Array
            const binaryString = atob(fingerprint);
            const bytes = new Uint8Array(binaryString.length);
            for (let i = 0; i < binaryString.length; i++) {
                bytes[i] = binaryString.charCodeAt(i);
            }

            const options: WebTransportOptions = {
                serverCertificateHashes: [
                    {
                        algorithm: 'sha-256',
                        value: bytes
                    }
                ]
            };

            this.transport = new wtGlobal(url, options);

            // Add a handshake timeout to detect Safari "hanging"
            const handshakeTimeout = new Promise((_, reject) => 
                setTimeout(() => reject(new Error('Handshake timeout (10s). Safari might not support certificate pinning on non-localhost IPs.')), 10000)
            );

            await Promise.race([this.transport.ready, handshakeTimeout]);
            
            log('[WebTransport] Connected successfully');
            this.isWebTransportActive = true;
            this.isConnecting = false;
            
            await this.setupControlStream();
            this.onConnected();
            this.receiveUnidirectionalStreams();

            this.transport.closed.then(() => {
                log('[WebTransport] Connection closed gracefully');
                this.cleanup();
            }).catch((e: Error) => {
                log(`[WebTransport] Connection closed with error: ${e.message}`);
                this.cleanup();
            });

        } catch (e) {
            const msg = (e as Error).message || 'Unknown error';
            log(`[WebTransport] Connection FAILED: ${msg}`);
            const isWS = window.websocketTransport?.isActive;
            if (!isWS && (msg.toLowerCase().includes('timeout') || msg.toLowerCase().includes('hash'))) {
                log('[WebTransport] Tip: If on Safari, try manually trusting the certificate in Keychain Access OR use the WebSocket fallback.');
            }
            this.isWebTransportActive = false;
            this.isConnecting = false;
        }
    }

    private cleanup() {
        this.isWebTransportActive = false;
        this.transport = null;
        this.controlStream = null;
        this.controlWriter = null;
    }

    private async setupControlStream() {
        if (!this.transport) return;
        try {
            this.controlStream = await this.transport.createBidirectionalStream();
            this.controlWriter = this.controlStream.writable.getWriter();
            this.receiveControlMessages(this.controlStream.readable);
            log('[WebTransport] Control stream established');
        } catch (e) {
            log(`[WebTransport] Failed to create control stream: ${(e as Error).message}`);
        }
    }

    private async receiveControlMessages(readable: ReadableStream) {
        const reader = readable.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done) break;
                buffer += decoder.decode(value, { stream: true });
                
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';
                for (const line of lines) {
                    if (line.trim()) {
                        try {
                            const msg = JSON.parse(line);
                            this.onJsonMessage(msg);
                        } catch (e) {
                            log(`[WebTransport] JSON parse error: ${(e as Error).message}`);
                        }
                    }
                }
            }
        } catch (e) {
            log(`[WebTransport] Control reader error: ${(e as Error).message}`);
        } finally {
            reader.releaseLock();
        }
    }

    private async receiveUnidirectionalStreams() {
        if (!this.transport) return;
        const reader = this.transport.incomingUnidirectionalStreams.getReader();
        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done) break;
                this.handleVideoStream(value);
            }
        } catch (e) {
            log(`[WebTransport] stream receiver error: ${(e as Error).message}`);
            this.isWebTransportActive = false;
        } finally {
            reader.releaseLock();
        }
    }

    private async handleVideoStream(stream: ReadableStream<Uint8Array>) {
        log('[WebTransport] Video stream received');
        const reader = stream.getReader();
        let buffer = new Uint8Array(10 * 1024 * 1024); // 10MB persistent buffer
        let offset = 0;

        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done) break;

                // Ensure buffer has enough space
                if (offset + value.length > buffer.length) {
                    const newSize = Math.max(buffer.length * 2, offset + value.length);
                    const newBuffer = new Uint8Array(newSize);
                    newBuffer.set(buffer.subarray(0, offset));
                    buffer = newBuffer;
                }

                buffer.set(value, offset);
                offset += value.length;

                let processed = 0;
                while (offset - processed >= 4) {
                    const dv = new DataView(buffer.buffer, buffer.byteOffset + processed, 4);
                    const packetLen = dv.getUint32(0, false);

                    if (offset - processed >= 4 + packetLen) {
                        const packetStart = processed + 4;
                        const packetEnd = packetStart + packetLen;
                        
                        // Copy only the required packet slice
                        const packet = buffer.slice(packetStart, packetEnd);
                        this.totalBytesReceived += 4 + packetLen;
                        this.frameCount++;
                        this.onBinaryMessage(packet.buffer);
                        
                        processed = packetEnd;
                    } else {
                        break;
                    }
                }

                if (processed > 0) {
                    if (processed < offset) {
                        buffer.copyWithin(0, processed, offset);
                    }
                    offset -= processed;
                }
            }
        } catch (e) {
            log(`[WebTransport] video reader error: ${(e as Error).message}`);
        } finally {
            reader.releaseLock();
        }
    }

    public async sendMsg(data: string) {
        if (!this.controlWriter || !this.isWebTransportActive) return;
        const encoder = new TextEncoder();
        try {
            await this.controlWriter.write(encoder.encode(data + '\n'));
        } catch (e) {
            log(`[WebTransport] Failed to send message: ${(e as Error).message}`);
        }
    }
}
