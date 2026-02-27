import { log, statusEl, videoEl, displayEl, ctx, updateStatusText } from './ui';

export class WebRTCManager {
    public rtcPeer: RTCPeerConnection | null = null;
    public isWebRtcActive = false;
    public fps = 0;

    private sendWs: (data: string) => void;
    private getNetworkLatencyVal: () => number;
    private lastVideoFrameTime = 0;
    private frameCount = 0;
    private lastTotalDecoded = 0;
    private lastStatsTime = 0;
    private lastFPSUpdate = Date.now();
    private getLatencyMonitor: () => number;
    private lastBytesReceived = 0;
    private bandwidthMbps = 0;
    private webrtcLatency = 0;
    private hasSentWebrtcReady = false;
    private statsInterval: ReturnType<typeof setInterval> | null = null;

    constructor(sendWs: (data: string) => void, getNetworkLatencyVal: () => number, getLatencyMonitor: () => number) {
        this.sendWs = sendWs;
        this.getNetworkLatencyVal = getNetworkLatencyVal;
        this.getLatencyMonitor = getLatencyMonitor;
    }

    public initWebRTC() {
        if (this.statsInterval) clearInterval(this.statsInterval);

        if (this.rtcPeer) {
            this.rtcPeer.close();
        }
        this.isWebRtcActive = false;
        this.lastTotalDecoded = -1;
        this.lastStatsTime = 0;
        this.hasSentWebrtcReady = false;
        this.rtcPeer = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
            bundlePolicy: 'max-bundle'
        });
        window.rtcPeer = this.rtcPeer;

        this.statsInterval = setInterval(() => this.pollStats(), 1000);

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
                this.sendWs(JSON.stringify({
                    type: 'webrtc_offer',
                    sdp: {
                        type: this.rtcPeer!.localDescription.type,
                        sdp: this.rtcPeer!.localDescription.sdp
                    }
                }));
            }
        });
    }

    private startVideoCanvasLoop = (_now: DOMHighResTimeStamp, metadata?: VideoFrameCallbackMetadata) => {
        if (!this.isWebRtcActive) return;
        if (ctx && videoEl.videoWidth > 0) {
            if (metadata) {
                // requestVideoFrameCallback natively throttles to the video frame rate.
                // Safari WebKit has a bug where metadata.mediaTime is always 0 for WebRTC,
                // so we simply count every callback invocation as a new frame.
                this.frameCount++;
            } else {
                // requestAnimationFrame fallback: deduplicate using currentTime
                if (videoEl.currentTime !== this.lastVideoFrameTime) {
                    this.lastVideoFrameTime = videoEl.currentTime;
                    this.frameCount++;
                }
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

    private pollStats() {
        if (!this.isWebRtcActive || !this.rtcPeer) return;
        this.rtcPeer.getStats(null).then(stats => {
            const now = Date.now();
            const deltaMs = this.lastStatsTime === 0 ? 1000 : now - this.lastStatsTime;
            this.lastStatsTime = now;

            let bytesReceived = 0;
            let framesDecoded = -1;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp' && report.kind === 'video') {
                    bytesReceived = report.bytesReceived;
                    if (report.framesDecoded !== undefined) {
                        framesDecoded = report.framesDecoded;
                    }
                }
                if (report.type === 'candidate-pair' && report.state === 'succeeded' && report.currentRoundTripTime !== undefined) {
                    this.webrtcLatency = Math.round(report.currentRoundTripTime * 1000);
                }
            });

            if (this.lastBytesReceived > 0 && bytesReceived > this.lastBytesReceived) {
                const deltaBytes = bytesReceived - this.lastBytesReceived;
                const bits = deltaBytes * 8;
                this.bandwidthMbps = bits / (deltaMs / 1000) / 1000000;
            } else if (this.lastBytesReceived > 0 && bytesReceived === this.lastBytesReceived) {
                // No new inbound video bytes since the previous sample window.
                // Treat as 0 Mbps for UI/testing instead of holding the last non-zero value.
                this.bandwidthMbps = 0;
            }
            this.lastBytesReceived = bytesReceived;

            if (framesDecoded !== -1) {
                if (this.lastTotalDecoded !== -1) {
                    const decodedDelta = framesDecoded - this.lastTotalDecoded;
                    this.fps = Math.round((decodedDelta * 1000) / deltaMs);
                }
                this.lastTotalDecoded = framesDecoded;
            }
        }).catch(() => { });
    }

    private updateStats() {
        const now = Date.now();
        const deltaMs = now - this.lastFPSUpdate;
        if (deltaMs >= 1000) {
            // Only use frameCount fallback if RTC stats haven't initialized
            if (this.lastTotalDecoded === -1) {
                this.fps = Math.round((this.frameCount * 1000) / deltaMs);
            }

            if (this.fps > 0 && !this.hasSentWebrtcReady && this.rtcPeer?.iceConnectionState === 'connected') {
                this.sendWs(JSON.stringify({ type: 'webrtc_ready' }));
                this.hasSentWebrtcReady = true;
            }

            this.frameCount = 0;
            this.lastFPSUpdate = now;

            const displayLatency = this.isWebRtcActive && this.webrtcLatency > 0 ? this.webrtcLatency : this.getLatencyMonitor();
            updateStatusText(this.isWebRtcActive, this.fps, displayLatency, this.getNetworkLatencyVal(), this.bandwidthMbps, videoEl.videoWidth, videoEl.videoHeight);
        }
    }

    public handleAnswer(sdp: RTCSessionDescriptionInit) {
        if (this.rtcPeer) this.rtcPeer.setRemoteDescription(new RTCSessionDescription(sdp));
    }

    public handleIce(candidate: RTCIceCandidateInit) {
        if (this.rtcPeer) this.rtcPeer.addIceCandidate(new RTCIceCandidate(candidate));
    }
}
