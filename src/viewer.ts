import { bandwidthSelect, configBtn, configDropdown, targetTypeRadios, qualitySlider, qualityValue } from './ui';
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
    () => network.networkLatency,
    () => network.wsBandwidthMbps
);

const webrtc: WebRTCManager = new WebRTCManager(
    (data) => network.sendMsg(data),
    () => network.networkLatency,
    () => webcodecs.latencyMonitor
);

setupInput((data) => network.sendMsg(data));

interface ConfigMessage {
    type: 'config';
    bandwidth?: number;
    quality?: number;
}

function sendConfig() {
    let target = 'bandwidth';
    for (const radio of targetTypeRadios) {
        if (radio.checked) {
            target = radio.value;
            break;
        }
    }

    const config: ConfigMessage = { type: 'config' };
    if (target === 'bandwidth') {
        config.bandwidth = parseInt(bandwidthSelect.value, 10);
    } else {
        config.quality = parseInt(qualitySlider.value, 10);
    }
    
    network.sendMsg(JSON.stringify(config));
    if (webrtc.isWebRtcActive) {
        webrtc.initWebRTC();
    }
}

if (configBtn && configDropdown) {
    configBtn.addEventListener('click', () => {
        configDropdown.classList.toggle('hidden');
    });
}

for (const radio of targetTypeRadios) {
    radio.addEventListener('change', () => {
        const isBandwidth = radio.value === 'bandwidth';
        bandwidthSelect.disabled = !isBandwidth;
        qualitySlider.disabled = isBandwidth;
        sendConfig();
    });
}

if (bandwidthSelect) {
    bandwidthSelect.addEventListener('change', sendConfig);
}

if (qualitySlider && qualityValue) {
    qualitySlider.addEventListener('input', (e) => {
        qualityValue.textContent = (e.target as HTMLInputElement).value;
    });
    qualitySlider.addEventListener('change', sendConfig);
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
