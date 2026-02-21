import { log, statusEl, videoEl, displayEl, ctx, updateStatusText } from './ui';

export class WebRTCManager {
    public rtcPeer: RTCPeerConnection | null = null;
    public isWebRtcActive = false;
    public fps = 0;

    private sendWs: (data: string) => void;
    private getNetworkLatencyVal: () => number;
    private lastVideoFrameTime = 0;
    private frameCount = 0;
    private lastFPSUpdate = Date.now();
    private getLatencyMonitor: () => number;

    constructor(sendWs: (data: string) => void, getNetworkLatencyVal: () => number, getLatencyMonitor: () => number) {
        this.sendWs = sendWs;
        this.getNetworkLatencyVal = getNetworkLatencyVal;
        this.getLatencyMonitor = getLatencyMonitor;
    }

    public initWebRTC() {
        this.rtcPeer = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
            bundlePolicy: 'max-bundle'
        });
        window.rtcPeer = this.rtcPeer;

        this.rtcPeer.onicecandidate = (e: RTCPeerConnectionIceEvent) => {
            if (e.candidate) {
                this.sendWs(JSON.stringify({ type: 'webrtc_ice', candidate: e.candidate }));
            }
        };

        this.rtcPeer.ontrack = (e: RTCTrackEvent) => {
            log('WebRTC track received');
            videoEl.srcObject = new MediaStream([e.track]);

            videoEl.play().then(() => {
                log('WebRTC Video playing');
                this.isWebRtcActive = true;
                if (statusEl) {
                    statusEl.textContent = 'WebRTC Connected';
                    statusEl.style.color = '#4bf';
                }
                this.startVideoCanvasLoop(0);
            }).catch((err: unknown) => {
                log('Video play error: ' + (err as Error).message);
            });
        };

        this.rtcPeer.oniceconnectionstatechange = () => {
            if (!this.rtcPeer) return;
            log('ICE state: ' + this.rtcPeer.iceConnectionState);
            if (this.rtcPeer.iceConnectionState === 'disconnected' || this.rtcPeer.iceConnectionState === 'failed') {
                this.isWebRtcActive = false;
                if (statusEl) {
                    statusEl.textContent = 'WebCodecs Fallback';
                    statusEl.style.color = '#fa4';
                }
            }
        };

        this.rtcPeer.addTransceiver('video', { direction: 'recvonly' });
        this.rtcPeer.createOffer().then((offer: RTCSessionDescriptionInit) => {
            if (offer.sdp) {
                offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* transport-cc\r\n/g, '');
                offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* goog-remb\r\n/g, '');
            }
            return this.rtcPeer!.setLocalDescription(offer);
        }).then(() => {
            if (this.rtcPeer!.localDescription) {
                this.sendWs(JSON.stringify({ type: 'webrtc_offer', sdp: this.rtcPeer!.localDescription.sdp }));
            }
        });
    }

    private startVideoCanvasLoop = (_now: DOMHighResTimeStamp, metadata?: VideoFrameCallbackMetadata) => {
        if (!this.isWebRtcActive) return;
        if (ctx && videoEl.videoWidth > 0) {
            if (metadata) {
                if (metadata.mediaTime !== this.lastVideoFrameTime) {
                    this.lastVideoFrameTime = metadata.mediaTime;
                    this.frameCount++;
                }
            } else {
                this.frameCount++;
            }

            if (displayEl.width !== videoEl.videoWidth || displayEl.height !== videoEl.videoHeight) {
                displayEl.width = videoEl.videoWidth;
                displayEl.height = videoEl.videoHeight;
            }
            ctx.drawImage(videoEl, 0, 0, displayEl.width, displayEl.height);
            this.updateStats();
        }

        if (videoEl.requestVideoFrameCallback) {
            videoEl.requestVideoFrameCallback(this.startVideoCanvasLoop);
        } else {
            requestAnimationFrame((now) => this.startVideoCanvasLoop(now));
        }
    }

    private updateStats() {
        const now = Date.now();
        if (now - this.lastFPSUpdate >= 1000) {
            this.fps = this.frameCount;
            this.frameCount = 0;
            this.lastFPSUpdate = now;
            updateStatusText(this.isWebRtcActive, this.fps, this.getLatencyMonitor(), this.getNetworkLatencyVal());
        }
    }

    public handleAnswer(sdp: RTCSessionDescriptionInit) {
        if (this.rtcPeer) this.rtcPeer.setRemoteDescription(new RTCSessionDescription(sdp));
    }

    public handleIce(candidate: RTCIceCandidateInit) {
        if (this.rtcPeer) this.rtcPeer.addIceCandidate(new RTCIceCandidate(candidate));
    }
}
