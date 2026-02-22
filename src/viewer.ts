import { bandwidthSelect } from './ui';
import { NetworkManager } from './network';
import { WebCodecsManager } from './webcodecs';
import { WebRTCManager } from './webrtc';
import { setupInput } from './input';

export { };

declare global {
    interface Window {
        getStats: () => { fps: number; latency: number; totalDecoded: number; webrtcFps: number; };
        hasReceivedKeyFrame: boolean;
        rtcPeer: RTCPeerConnection | null;
    }
}

const network = new NetworkManager(
    handleBinaryMessage,
    handleJsonMessage,
    () => { webrtc.initWebRTC(); }
);

const webcodecs: WebCodecsManager = new WebCodecsManager(
    () => webrtc.isWebRtcActive,
    () => network.networkLatency
);

const webrtc: WebRTCManager = new WebRTCManager(
    (data) => network.sendMsg(data),
    () => network.networkLatency,
    () => webcodecs.latencyMonitor
);

setupInput((data) => network.sendMsg(data));

// Hook up bandwidth select if we want to send it to the server in the future
if (bandwidthSelect) {
    bandwidthSelect.addEventListener('change', (e) => {
        const value = (e.target as HTMLSelectElement).value;
        network.sendMsg(JSON.stringify({ type: 'config', bandwidth: parseInt(value, 10) }));
        if (webrtc.isWebRtcActive) {
            webrtc.initWebRTC();
        }
    });
}

function handleBinaryMessage(buffer: ArrayBuffer) {
    const dv = new DataView(buffer);
    const type = dv.getUint8(0);

    if (type === 1) { // Video
        const timestamp = dv.getFloat64(1, false);
        const chunkData = new Uint8Array(buffer, 9);

        const now = Date.now();
        webcodecs.latencyMonitor = Math.round(Math.abs(now - timestamp));

        const isKey = (chunkData[0] & 0x01) === 0;
        if (isKey) {
            window.hasReceivedKeyFrame = true;
        }

        if (!window.hasReceivedKeyFrame) return;
        if (webrtc.isWebRtcActive) return;

        webcodecs.decodeChunk(isKey, timestamp, chunkData);
    }
}

function handleJsonMessage(msg: Record<string, unknown>) {
    if (msg.type === 'webrtc_answer') {
        webrtc.handleAnswer(msg.sdp as RTCSessionDescriptionInit);
    } else if (msg.type === 'webrtc_ice' && msg.candidate) {
        webrtc.handleIce(msg.candidate as RTCIceCandidateInit);
    }
}

window.getStats = () => {
    let webrtcTotal = 0;
    const v = document.getElementById('webrtc-video') as HTMLVideoElement;
    if (v && v.getVideoPlaybackQuality) {
        webrtcTotal = v.getVideoPlaybackQuality().totalVideoFrames;
    }
    return {
        fps: webrtc.isWebRtcActive ? webrtc.fps : webcodecs.fps,
        latency: webcodecs.latencyMonitor,
        totalDecoded: webrtc.isWebRtcActive ? webrtcTotal : webcodecs.totalDecoded,
        webrtcFps: webrtc.fps
    };
};
