export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
    }
}

const statusEl = document.getElementById('status') as HTMLDivElement;
const displayEl = document.getElementById('display') as HTMLCanvasElement;
const videoEl = document.getElementById('webrtc-video') as HTMLVideoElement;
const overlayEl = document.getElementById('input-overlay') as HTMLDivElement;
const debugEl = document.getElementById('debug-log') as HTMLDivElement;

const ctx = displayEl.getContext('2d', { alpha: false, desynchronized: true });

function log(msg: string) {
    console.log(msg);
    if (!debugEl) return;
    const line = document.createElement('div');
    line.textContent = msg;
    debugEl.appendChild(line);
    debugEl.scrollTop = debugEl.scrollHeight;
}

// --- WebCodecs Setup ---

let decoder: VideoDecoder | null = null;
let frameCount = 0;
let lastFPSUpdate = Date.now();
let fps = 0;
let latencyMonitor = 0; // ms
let networkLatency = 0;
let totalDecoded = 0;

// Expose stats for Playwright
window.getStats = () => ({ fps, latency: latencyMonitor, totalDecoded, webrtcFps: fps });

let isInitializing = false;
let decoderInitTimeout: ReturnType<typeof setTimeout> | null = null;

function initDecoder() {
    if (isInitializing) return;
    isInitializing = true;

    if (decoderInitTimeout !== null) {
        clearTimeout(decoderInitTimeout);
        decoderInitTimeout = null;
    }

    if (!('VideoDecoder' in window)) {
        log('WebCodecs API not supported. Use Chrome or Edge.');
        if (statusEl) statusEl.textContent = 'WebCodecs Not Supported';
        isInitializing = false;
        return;
    }

    if (decoder) {
        try {
            if (decoder.state !== 'closed') decoder.close();
        } catch (e: unknown) {
            if (e instanceof Error) {
                console.warn('Error closing decoder:', e.message);
            }
        }
    }

    try {
        decoder = new VideoDecoder({
            output: handleFrame,
            error: (e: Error) => {
                log(`Decoder Error: ${e.message}`);
                console.error('VideoDecoder Error Details:', e);
                if (statusEl) statusEl.textContent = `Decoder Err: ${e.message}`;

                // Schedule re-init, don't do it synchronously
                if (decoderInitTimeout === null) {
                    decoderInitTimeout = setTimeout(() => {
                        decoderInitTimeout = null;
                        initDecoder();
                    }, 100);
                }
            }
        });

        // Configure for VP8
        decoder.configure({
            codec: 'vp8',
            optimizeForLatency: true,
            hardwareAcceleration: 'prefer-software'
        });

        // CRITICAL: Reset keyframe flag so we don't feed delta frames to new decoder
        window.hasReceivedKeyFrame = false;

        log('Decoder initialized (vp8). Waiting for Keyframe...');
    } catch (e: unknown) {
        if (e instanceof Error) {
            log('Failed to initialize decoder: ' + e.message);
            if (statusEl) statusEl.textContent = 'Decoder Init Error';
            console.error(e);
        }
    } finally {
        isInitializing = false;
    }
}

function handleFrame(frame: VideoFrame) {
    if (totalDecoded === 0) {
        log('First frame decoded successfully!');
        console.log('Frame Format:', frame.format, frame.codedWidth, frame.codedHeight);
    }
    totalDecoded++;

    if (ctx && frame.displayWidth && frame.displayHeight) {
        // Resize canvas if needed
        if (displayEl.width !== frame.displayWidth || displayEl.height !== frame.displayHeight) {
            displayEl.width = frame.displayWidth;
            displayEl.height = frame.displayHeight;
        }
        ctx.drawImage(frame as CanvasImageSource, 0, 0, displayEl.width, displayEl.height);
    }

    frame.close(); // Mandatory cleanup!

    frameCount++;
    updateStats();
}

function updateStats() {
    const now = Date.now();
    if (now - lastFPSUpdate >= 1000) {
        fps = frameCount;
        frameCount = 0;
        lastFPSUpdate = now;
        updateStatusText();
    }
}

function updateStatusText() {
    if (!statusEl) return;
    const codecInfo = isWebRtcActive ? '[WebRTC VP8]' : '[WebCodecs VP8]';
    statusEl.textContent = `${codecInfo} | FPS: ${fps} | Latency (Video): ${latencyMonitor}ms | Ping: ${networkLatency}ms`;
}

initDecoder();

// --- WebRTC Setup ---
let rtcPeer: RTCPeerConnection | null = null;
let isWebRtcActive = false;

function initWebRTC() {
    rtcPeer = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
        bundlePolicy: 'max-bundle'
    });
    window.rtcPeer = rtcPeer;

    rtcPeer.onicecandidate = (e: RTCPeerConnectionIceEvent) => {
        if (e.candidate && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'webrtc_ice', candidate: e.candidate }));
        }
    };

    rtcPeer.ontrack = (e: RTCTrackEvent) => {
        log('WebRTC track received');
        videoEl.srcObject = new MediaStream([e.track]);

        videoEl.play().then(() => {
            log('WebRTC Video playing');
            isWebRtcActive = true;
            if (statusEl) {
                statusEl.textContent = 'WebRTC Connected';
                statusEl.style.color = '#4bf';
            }
            startVideoCanvasLoop(0);
        }).catch((err: unknown) => {
            if (err instanceof Error) {
                log('Video play error: ' + err.message);
            }
        });
    };

    rtcPeer.oniceconnectionstatechange = () => {
        if (!rtcPeer) return;
        log('ICE state: ' + rtcPeer.iceConnectionState);
        if (rtcPeer.iceConnectionState === 'disconnected' || rtcPeer.iceConnectionState === 'failed') {
            isWebRtcActive = false;
            if (statusEl) {
                statusEl.textContent = 'WebCodecs Fallback';
                statusEl.style.color = '#fa4';
            }
        }
    };

    rtcPeer.addTransceiver('video', { direction: 'recvonly' });
    rtcPeer.createOffer().then((offer: RTCSessionDescriptionInit) => {
        if (offer.sdp) {
            // Strip Congestion Control extensions
            offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* transport-cc\r\n/g, '');
            offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* goog-remb\r\n/g, '');
        }
        return rtcPeer!.setLocalDescription(offer); // using ! because we just created rtcPeer
    }).then(() => {
        if (ws.readyState === WebSocket.OPEN && rtcPeer!.localDescription) {
            ws.send(JSON.stringify({ type: 'webrtc_offer', sdp: rtcPeer!.localDescription.sdp }));
        }
    });
}

let lastVideoFrameTime = 0;



function startVideoCanvasLoop(_now: DOMHighResTimeStamp, metadata?: VideoFrameCallbackMetadata) {
    if (!isWebRtcActive) return;
    if (ctx && videoEl.videoWidth > 0) {
        if (metadata) {
            if (metadata.mediaTime !== lastVideoFrameTime) {
                lastVideoFrameTime = metadata.mediaTime;
                frameCount++;
            }
        } else {
            frameCount++; // Fallback ticking
        }

        if (displayEl.width !== videoEl.videoWidth || displayEl.height !== videoEl.videoHeight) {
            displayEl.width = videoEl.videoWidth;
            displayEl.height = videoEl.videoHeight;
        }
        ctx.drawImage(videoEl, 0, 0, displayEl.width, displayEl.height);
        updateStats();
    }

    // In Edge/Chrome
    if (videoEl.requestVideoFrameCallback) {
        videoEl.requestVideoFrameCallback(startVideoCanvasLoop);
    } else {
        requestAnimationFrame((now) => startVideoCanvasLoop(now));
    }
}

// --- Network ---

const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const wsUrl = `${protocol}//${window.location.host}`;
log(`Connecting to ${wsUrl}...`);

const ws = new WebSocket(wsUrl);
ws.binaryType = 'arraybuffer';

ws.onopen = () => {
    log('WebSocket Connected');
    if (statusEl) {
        statusEl.textContent = 'Connected, Negotiating WebRTC...';
        statusEl.style.color = '#4f4';
    }

    // Start ping loop
    setInterval(sendPing, 1000);

    // Initiate WebRTC
    initWebRTC();
};

ws.onclose = () => {
    log('WebSocket Disconnected');
    if (statusEl) {
        statusEl.textContent = 'Disconnected';
        statusEl.style.color = '#f44';
    }
};

ws.onerror = (err: Event) => {
    log('WebSocket Error');
    console.error(err);
};

ws.onmessage = (event: MessageEvent) => {
    if (event.data instanceof ArrayBuffer) {
        handleBinaryMessage(event.data);
    } else if (typeof event.data === 'string') {
        handleJsonMessage(event.data);
    }
};

function handleBinaryMessage(buffer: ArrayBuffer) {
    const dv = new DataView(buffer);
    const type = dv.getUint8(0);

    if (type === 1) { // Video
        const timestamp = dv.getFloat64(1, false); // Big Endian
        const chunkData = new Uint8Array(buffer, 9); // VP8 Frame

        const now = Date.now();
        latencyMonitor = now - timestamp;

        const isKey = (chunkData[0] & 0x01) === 0;

        if (isKey) {
            window.hasReceivedKeyFrame = true;
        }

        if (!window.hasReceivedKeyFrame) {
            return;
        }

        if (isWebRtcActive) {
            return;
        }

        if (decoder && decoder.state === 'configured') {
            try {
                decoder.decode(new EncodedVideoChunk({
                    type: isKey ? 'key' : 'delta',
                    timestamp: timestamp * 1000,
                    data: chunkData
                }));
            } catch (e: unknown) {
                if (e instanceof Error) {
                    console.error('Decode exception:', e.message);
                    if (statusEl) statusEl.textContent = 'Decode Exc: ' + e.message;
                }
                if (!isInitializing && decoderInitTimeout === null) {
                    initDecoder();
                }
            }
        } else {
            if (decoder && (decoder.state === 'closed' || decoder.state === 'unconfigured')) {
                if (!isInitializing && decoderInitTimeout === null) {
                    log('Decoder stuck/closed. Re-initializing...');
                    initDecoder();
                }
            }
        }
    }
}

// Types for incoming JSON messages
interface BaseSignalingMessage {
    type: string;
}

interface PongMessage extends BaseSignalingMessage {
    type: 'pong';
    timestamp: number;
}

interface WebRTCAnswerMessage extends BaseSignalingMessage {
    type: 'webrtc_answer';
    sdp: RTCSessionDescriptionInit;
}

interface WebRTCIceMessage extends BaseSignalingMessage {
    type: 'webrtc_ice';
    candidate: RTCIceCandidateInit;
}

type SignalingMessage = PongMessage | WebRTCAnswerMessage | WebRTCIceMessage;

function handleJsonMessage(data: string) {
    try {
        const msg = JSON.parse(data) as SignalingMessage;
        if (msg.type === 'pong') {
            networkLatency = Date.now() - msg.timestamp;
            updateStats();
        } else if (msg.type === 'webrtc_answer') {
            if (rtcPeer) rtcPeer.setRemoteDescription(new RTCSessionDescription(msg.sdp));
        } else if (msg.type === 'webrtc_ice' && msg.candidate) {
            if (rtcPeer) rtcPeer.addIceCandidate(new RTCIceCandidate(msg.candidate));
        }
    } catch (e: unknown) {
        // Ignored
    }
}

function sendPing() {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping', timestamp: Date.now() }));
    }
}

function spawn(command: string) {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'spawn', command }));
    }
}

// --- UI Actions ---
// Hook up the buttons explicitly instead of using inline onclick attributes
const buttons = document.querySelectorAll('.launcher-btn');
buttons.forEach(btn => {
    btn.addEventListener('click', () => {
        const cmd = btn.getAttribute('data-spawn');
        if (cmd) {
            spawn(cmd);
        }
    });
});

// --- Input Handling ---

window.addEventListener('keydown', (event: KeyboardEvent) => {
    if (['Space', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Tab', 'Backspace', 'Enter', 'Escape', 'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10', 'F11', 'F12'].includes(event.code) || event.key === ' ') {
        event.preventDefault();
    }
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'keydown', key: event.key }));
    }
});

window.addEventListener('keyup', (event: KeyboardEvent) => {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'keyup', key: event.key }));
    }
});

const sendMouse = (type: string, x: number | null, y: number | null, button: number | null) => {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type, x, y, button }));
    }
};

if (overlayEl) {
    overlayEl.addEventListener('mousemove', (e: MouseEvent) => {
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
