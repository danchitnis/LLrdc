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
    private lastCanvasFrameTime = 0;
    private getLatencyMonitor: () => number;
    private bandwidthMbps = 0;
    private webrtcLatency = 0;
    private hasSentWebrtcReady = false;
    private statsInterval: ReturnType<typeof setInterval> | null = null;
    private iceCandidatesBuffer: RTCIceCandidateInit[] = [];

    constructor(sendWs: (data: string) => void, getNetworkLatencyVal: () => number, getLatencyMonitor: () => number) {
        console.log('[WebRTCManager] Constructor called');
        this.sendWs = sendWs;
        this.getNetworkLatencyVal = getNetworkLatencyVal;
        this.getLatencyMonitor = getLatencyMonitor;

        // Unmute video element upon user interaction
        const unmuteHandler = () => {
            if (videoEl && videoEl.muted) {
                videoEl.muted = false;
                log('User interacted: unmuted WebRTC audio');
                document.body.removeEventListener('click', unmuteHandler);
                document.body.removeEventListener('keydown', unmuteHandler);
            }
        };
        document.body.addEventListener('click', unmuteHandler);
        document.body.addEventListener('keydown', unmuteHandler);
    }

    public initWebRTC() {
        if (this.statsInterval) clearInterval(this.statsInterval);

        if (this.rtcPeer) {
            this.rtcPeer.close();
        }
        
        // Always clear srcObject to ensure a fresh MediaStream is created for the new session
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
        this.lastCanvasFrameTime = 0;
        this.hasSentWebrtcReady = false;
        
        const isLocalhost = window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1';
        const iceServersConfig = isLocalhost ? [] : [{ urls: 'stun:stun.l.google.com:19302' }];

        this.rtcPeer = new RTCPeerConnection({
            iceServers: iceServersConfig,
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

        this.rtcPeer.onconnectionstatechange = () => {
            if (this.rtcPeer) {
                log('Connection state: ' + this.rtcPeer.connectionState);
                if (this.rtcPeer.connectionState === 'connected') {
                    this.rtcPeer.getReceivers().forEach(receiver => {
                        if ('playoutDelayHint' in receiver) (receiver as any).playoutDelayHint = 0;
                        if ('jitterBufferTarget' in receiver) (receiver as any).jitterBufferTarget = 0;
                    });
                }
            }
        };

        this.rtcPeer.ontrack = (e: RTCTrackEvent) => {
            log('WebRTC track received: ' + e.track.kind);
            
            // Apply low-latency hints immediately to the receiver
            if ('playoutDelayHint' in e.receiver) (e.receiver as any).playoutDelayHint = 0;
            if ('jitterBufferTarget' in e.receiver) (e.receiver as any).jitterBufferTarget = 0;

            let stream = videoEl.srcObject as MediaStream;
            if (!stream) {
                stream = new MediaStream();
                videoEl.srcObject = stream;
            }
            stream.addTrack(e.track);

            // Listen for resolution changes (e.g., recovering from 2x2 placeholder)
            videoEl.onresize = () => {
                log(`Video resolution changed: ${videoEl.videoWidth}x${videoEl.videoHeight}`);
                if (videoEl.videoWidth > 4 && videoEl.videoHeight > 4) {
                    displayEl.width = videoEl.videoWidth;
                    displayEl.height = videoEl.videoHeight;
                    if (sharpnessLayerEl) {
                        sharpnessLayerEl.width = videoEl.videoWidth;
                        sharpnessLayerEl.height = videoEl.videoHeight;
                    }
                }
            };

            this.isWebRtcActive = true;
            videoEl.play().then(() => {
                log('WebRTC Video/Audio playing');
                this.startVideoCanvasLoop(0);
            }).catch((err: unknown) => {
                log('Media play error: ' + (err as Error).message);
                // Still active even if play() was interrupted, as stats will still flow
                this.startVideoCanvasLoop(0);
            });
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

    private startVideoCanvasLoop = (now: DOMHighResTimeStamp, metadata?: VideoFrameCallbackMetadata) => {
        if (!this.isWebRtcActive) return;
        if (ctx && videoEl.videoWidth > 0) {
            this.lastCanvasFrameTime = Date.now();
            if (metadata) {
                this.frameCount++;
                
                // Expose high-precision latency metadata for benchmarks
                (window as any).__llrdcLatestFrameMeta = {
                    callbackAtMs: performance.timeOrigin + now,
                    presentationAtMs: performance.timeOrigin + metadata.presentationTime,
                    expectedDisplayAtMs: performance.timeOrigin + metadata.expectedDisplayTime,
                    receiveAtMs: performance.timeOrigin + (metadata.receiveTime || metadata.presentationTime), // Fallback if receiveTime is missing
                    processingDurationMs: (metadata.processingDuration || 0) * 1000,
                    presentedFrames: metadata.presentedFrames
                };
            } else {
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
            return;
        }
        this.rtcPeer.getStats(null).then(stats => {
            const now = Date.now();
            const deltaMs = this.lastStatsTime === 0 ? 1000 : now - this.lastStatsTime;
            this.lastStatsTime = now;

            let bytesReceived = 0;
            let audioBytes = 0;
            let hasAudioTrack = false;
            let framesDecoded = -1;
            stats.forEach(report => {
                if (report.type === 'inbound-rtp') {
                    const kind = report.kind || report.mediaType;
                    if (kind === 'video') {
                        bytesReceived = report.bytesReceived || 0;
                        if (report.framesDecoded !== undefined) {
                            framesDecoded = report.framesDecoded;
                        } else {
                            console.log('[pollStats] inbound-rtp video found but framesDecoded is undefined');
                        }
                    } else if (kind === 'audio') {
                        hasAudioTrack = true;
                        audioBytes = report.bytesReceived || 0;
                    }
                }
                if (report.type === 'candidate-pair' && report.state === 'succeeded' && report.currentRoundTripTime !== undefined) {
                    this.webrtcLatency = Math.round(report.currentRoundTripTime * 1000);
                }
            });

            if (framesDecoded === -1) {
                // Periodically log that we haven't found frames yet if bytes are flowing
                if (bytesReceived > 0 && Math.random() < 0.1) {
                    console.log('[pollStats] video bytes flowing but no framesDecoded found in stats');
                }
            }

            (window as any).audioStats = {
                hasAudioTrack: hasAudioTrack,
                bytesReceived: audioBytes
            };

            if (bytesReceived === 0) {
                stats.forEach(report => {
                    if (report.type === 'inbound-rtp' && typeof report.bytesReceived === 'number' && report.bytesReceived > 0) {
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
                this.bandwidthMbps = 0;
            }
            this.lastBytesReceived = bytesReceived;

            const isCanvasActive = (Date.now() - this.lastCanvasFrameTime) < 2000;

            if (framesDecoded !== -1) {
                if (this.lastTotalDecoded !== -1) {
                    const decodedDelta = framesDecoded - this.lastTotalDecoded;
                    this.fps = Math.round((decodedDelta * 1000) / deltaMs);
                }
                this.lastTotalDecoded = framesDecoded;
            }

            // Only update UI from pollStats if the canvas loop is NOT active (e.g. background tab)
            if (!isCanvasActive) {
                const displayLatency = this.isWebRtcActive && this.webrtcLatency > 0 ? this.webrtcLatency : this.getLatencyMonitor();
                updateStatusText(this.isWebRtcActive, this.fps, displayLatency, this.getNetworkLatencyVal(), this.bandwidthMbps, videoEl.videoWidth, videoEl.videoHeight, this.videoCodec);
            }
        }).catch(() => { });
    }

    private updateStats() {
        const now = Date.now();
        const deltaMs = now - this.lastFPSUpdate;
        if (deltaMs >= 1000) {
            // We now rely on pollStats to calculate WebRTC FPS based on framesDecoded

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
        if (this.rtcPeer) {
            console.log('[WebRTCManager] Received WebRTC answer, setting remote description');
            this.rtcPeer.setRemoteDescription(new RTCSessionDescription(sdp)).then(() => {
                console.log('[WebRTCManager] Remote description set, processing buffered ICE candidates:', this.iceCandidatesBuffer.length);
                this.iceCandidatesBuffer.forEach(candidate => {
                    this.rtcPeer?.addIceCandidate(new RTCIceCandidate(candidate)).catch(err => {
                        console.error('[WebRTCManager] Error adding buffered ICE candidate:', err);
                    });
                });
                this.iceCandidatesBuffer = [];
            }).catch(err => {
                console.error('[WebRTCManager] Error setting remote description:', err);
                log('WebRTC remote description error: ' + err.message);
            });
        }
    }

    public handleIce(candidate: RTCIceCandidateInit) {
        if (this.rtcPeer) {
            if (this.rtcPeer.remoteDescription) {
                this.rtcPeer.addIceCandidate(new RTCIceCandidate(candidate)).catch(err => {
                    console.error('[WebRTCManager] Error adding remote ICE candidate:', err);
                });
            } else {
                this.iceCandidatesBuffer.push(candidate);
            }
        }
    }
}
