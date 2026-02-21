import { log, statusEl } from './ui';

export class NetworkManager {
    public ws: WebSocket;
    public networkLatency = 0;

    private onBinaryMessage: (buffer: ArrayBuffer) => void;
    private onJsonMessage: (msg: Record<string, unknown>) => void;
    private onOpenCallback: () => void;

    constructor(onBinaryMessage: (buffer: ArrayBuffer) => void, onJsonMessage: (msg: Record<string, unknown>) => void, onOpenCallback: () => void) {
        this.onBinaryMessage = onBinaryMessage;
        this.onJsonMessage = onJsonMessage;
        this.onOpenCallback = onOpenCallback;

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}`;
        log(`Connecting to ${wsUrl}...`);

        this.ws = new WebSocket(wsUrl);
        this.ws.binaryType = 'arraybuffer';

        this.ws.onopen = () => {
            log('WebSocket Connected');
            if (statusEl) {
                statusEl.textContent = 'Connected, Negotiating WebRTC...';
                statusEl.style.color = '#4f4';
            }
            setInterval(() => this.sendPing(), 1000);
            this.onOpenCallback();
        };

        this.ws.onclose = () => {
            log('WebSocket Disconnected');
            if (statusEl) {
                statusEl.textContent = 'Disconnected';
                statusEl.style.color = '#f44';
            }
        };

        this.ws.onerror = (err: Event) => {
            log('WebSocket Error');
            console.error(err);
        };

        this.ws.onmessage = (event: MessageEvent) => {
            if (event.data instanceof ArrayBuffer) {
                this.onBinaryMessage(event.data);
            } else if (typeof event.data === 'string') {
                try {
                    const msg = JSON.parse(event.data) as Record<string, unknown>;
                    if (msg.type === 'pong') {
                        this.networkLatency = Date.now() - (msg.timestamp as number);
                    } else {
                        this.onJsonMessage(msg);
                    }
                } catch {
                    // Ignored
                }
            }
        };
    }

    private sendPing() {
        if (this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type: 'ping', timestamp: Date.now() }));
        }
    }

    public sendMsg(data: string) {
        if (this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(data);
        }
    }
}
