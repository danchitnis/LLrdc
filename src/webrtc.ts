import { log, statusEl, videoEl, displayEl, sharpnessLayerEl, ctx, updateStatusText, applySmoothingSettings } from './ui';

export class WebRTCManager {
    public rtcPeer: RTCPeerConnection | null = null;
    public inputChannel: RTCDataChannel | null = null;
    public isWebRtcActive = false;
    public fps = 0;
    public videoCodec = 'vp8';

    private sendWs: (data: string) => void;
    private getNetworkLatencyVal: () => number;
    private lastVideoFrameTime = 0;
    private frameCount = 0;
    public lastTotalDecoded = -1;
    public lastBytesReceived = 0;
    private lastStatsTime = 0;
    private lastFPSUpdate = Date.now();
    private getLatencyMonitor: () => number;
    private bandwidthMbps = 0;
    private webrtcLatency = 0;
    private hasSentWebrtcReady = false;
    private statsInterval: ReturnType<typeof setInterval> | null = null;

    constructor(sendWs: (data: string) => void, getNetworkLatencyVal: () => number, getLatencyMonitor: () => number) {
        console.log('[WebRTCManager] Constructor called');
        this.sendWs = sendWs;
        this.getNetworkLatencyVal = getNetworkLatencyVal;
        this.getLatencyMonitor = getLatencyMonitor;
    }

    public initWebRTC() {
        console.log('[WebRTCManager] initWebRTC called');
        if (this.statsInterval) clearInterval(this.statsInterval);

        if (this.rtcPeer) {
            this.rtcPeer.close();
        }
        if (videoEl) {
            videoEl.srcObject = null;
        }
        this.isWebRtcActive = false;
        this.lastTotalDecoded = -1;
        this.lastStatsTime = 0;
        this.lastBytesReceived = 0;
        this.bandwidthMbps = 0;
        this.frameCount = 0;
        this.lastVideoFrameTime = 0;
        this.hasSentWebrtcReady = false;
        this.rtcPeer = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
            bundlePolicy: 'max-bundle'
        });
        window.rtcPeer = this.rtcPeer;

        this.inputChannel = this.rtcPeer.createDataChannel('input', {
            ordered: false,
            maxRetransmits: 0
        });

        this.inputChannel.onopen = () => log('Input DataChannel Opened');
        this.inputChannel.onclose = () => log('Input DataChannel Closed');

        this.statsInterval = setInterval(() => this.pollStats(), 1000);

        (window as any).getStats = () => {
            return {
                fps: this.fps,
                bandwidth: this.bandwidthMbps,
                bytesReceived: this.lastBytesReceived,
                latency: this.webrtcLatency,
                totalDecoded: this.lastTotalDecoded
            };
        };

        this.rtcPeer.onicecandidate = (e: RTCPeerConnectionIceEvent) => {
            if (e.candidate) {
                this.sendWs(JSON.stringify({ type: 'webrtc_ice', candidate: e.candidate }));
            }
        };

        this.rtcPeer.ontrack = (e: RTCTrackEvent) => {
            log('WebRTC track received: ' + e.track.kind);
            let stream = videoEl.srcObject as MediaStream;
            if (!stream) {
                stream = new MediaStream();
                videoEl.srcObject = stream;
            }
            stream.addTrack(e.track);

            if (videoEl.paused) {
                this.isWebRtcActive = true;
                videoEl.play().then(() => {
                    log('WebRTC Video/Audio playing');
                    if (statusEl) {
                        statusEl.textContent = 'WebRTC Connected';
                    }
                    this.startVideoCanvasLoop(0);
                }).catch((err: unknown) => {
                    log('Media play error: ' + (err as Error).message);
                    // Still active even if play() was interrupted, as stats will still flow
                    this.startVideoCanvasLoop(0);
                });
            }
        };

        this.rtcPeer.oniceconnectionstatechange = () => {
            if (!this.rtcPeer) return;
            log('ICE state: ' + this.rtcPeer.iceConnectionState);
            if (this.rtcPeer.iceConnectionState === 'disconnected' || this.rtcPeer.iceConnectionState === 'failed') {
                this.isWebRtcActive = false;
                if (statusEl) {
                    statusEl.textContent = 'WebCodecs Fallback';
                }
            }
        };

        this.rtcPeer.addTransceiver('video', { direction: 'recvonly' });
        this.rtcPeer.addTransceiver('audio', { direction: 'recvonly' });
        this.rtcPeer.createOffer().then((offer: RTCSessionDescriptionInit) => {
            if (offer.sdp) {
                offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* transport-cc\r\n/g, '');
                offer.sdp = offer.sdp.replace(/a=rtcp-fb:\d* goog-remb\r\n/g, '');
            }
            return this.rtcPeer!.setLocalDescription(offer);
        }).then(() => {
            if (this.rtcPeer!.localDescription) {
                log('Sending WebRTC offer...');
                this.sendWs(JSON.stringify({
                    type: 'webrtc_offer',
                    sdp: {
                        type: this.rtcPeer!.localDescription.type,
                        sdp: this.rtcPeer!.localDescription.sdp
                    }
                }));
            }
        }).catch((err: unknown) => {
            log('WebRTC createOffer/setLocalDescription error: ' + (err as Error).message);
            console.error('WebRTC Error:', err);
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
                if (sharpnessLayerEl) {
                    sharpnessLayerEl.width = videoEl.videoWidth;
                    sharpnessLayerEl.height = videoEl.videoHeight;
                }
                applySmoothingSettings();
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
        if (!this.rtcPeer) {
            console.log('[pollStats] No rtcPeer');
            return;
        }
        if (!this.isWebRtcActive) {
            // console.log('[pollStats] isWebRtcActive is false');
            return;
        }
        this.rtcPeer.getStats(null).then(stats => {
            const now = Date.now();
            const deltaMs = this.lastStatsTime === 0 ? 1000 : now - this.lastStatsTime;
            this.lastStatsTime = now;

            let bytesReceived = 0;
            let framesDecoded = -1;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp') {
                    const kind = report.kind || report.mediaType;
                    if (kind === 'video') {
                        bytesReceived = report.bytesReceived || 0;
                        if (report.framesDecoded !== undefined) {
                            framesDecoded = report.framesDecoded;
                        }
                    }
                }
                if (report.type === 'candidate-pair' && report.state === 'succeeded' && report.currentRoundTripTime !== undefined) {
                    this.webrtcLatency = Math.round(report.currentRoundTripTime * 1000);
                }
            });

            // Fallback for some browsers/versions where kind might be missing in top level
            if (bytesReceived === 0) {
                stats.forEach(report => {
                    if (report.type === 'inbound-rtp' && typeof report.bytesReceived === 'number' && report.bytesReceived > 0) {
                        // If we have multiple, this might sum audio + video but at least it's not 0
                        bytesReceived += report.bytesReceived;
                    }
                });
            }

            if (this.lastBytesReceived > 0 && bytesReceived > this.lastBytesReceived) {
                const deltaBytes = bytesReceived - this.lastBytesReceived;
                const bits = deltaBytes * 8;
                this.bandwidthMbps = bits / (deltaMs / 1000) / 1000000;
            } else if (this.lastBytesReceived > 0 && bytesReceived === this.lastBytesReceived) {
                this.bandwidthMbps = 0;
            } else if (bytesReceived > 0 && this.lastBytesReceived === 0) {
                // First non-zero sample
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
            updateStatusText(this.isWebRtcActive, this.fps, displayLatency, this.getNetworkLatencyVal(), this.bandwidthMbps, videoEl.videoWidth, videoEl.videoHeight, this.videoCodec);
        }
    }

    public handleAnswer(sdp: RTCSessionDescriptionInit) {
        if (this.rtcPeer) this.rtcPeer.setRemoteDescription(new RTCSessionDescription(sdp));
    }

    public handleIce(candidate: RTCIceCandidateInit) {
        if (this.rtcPeer) this.rtcPeer.addIceCandidate(new RTCIceCandidate(candidate));
    }
}
