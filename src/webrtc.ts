import { log, statusEl, videoEl, displayEl, sharpnessLayerEl, ctx, applySmoothingSettings } from './ui';
import type { PresentedFrameMeta } from './client/types';

interface LowLatencyReceiver {
    playoutDelayHint?: number;
    jitterBufferTarget?: number | null;
}

interface WebRTCDebugWindow extends Window {
    __llrdcLatestFrameMeta?: PresentedFrameMeta;
    audioStats?: {
        hasAudioTrack: boolean;
        bytesReceived: number;
    };
}

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
    private onPresentedFrame?: (frame: PresentedFrameMeta) => void;
    public bandwidthMbps = 0;
    private smoothedBandwidth = 0;
    private smoothedFps = 0;
    private webrtcLatency = 0;
    private hasSentWebrtcReady = false;
    private statsInterval: ReturnType<typeof setInterval> | null = null;
    private iceCandidatesBuffer: RTCIceCandidateInit[] = [];
    private canvasLoopGeneration = 0;

    constructor(sendWs: (data: string) => void, getNetworkLatencyVal: () => number, getLatencyMonitor: () => number, onPresentedFrame?: (frame: PresentedFrameMeta) => void) {
        console.log('[WebRTCManager] Constructor called');
        this.sendWs = sendWs;
        this.getNetworkLatencyVal = getNetworkLatencyVal;
        this.getLatencyMonitor = getLatencyMonitor;
        this.onPresentedFrame = onPresentedFrame;

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

    public resetStats() {
        this.fps = 0;
        this.lastTotalDecoded = -1;
        this.lastStatsTime = 0;
        this.lastBytesReceived = 0;
        this.bandwidthMbps = 0;
        this.frameCount = 0;
        this.lastFPSUpdate = Date.now();
        this.lastVideoFrameTime = 0;
        this.lastCanvasFrameTime = 0;
        this.smoothedBandwidth = 0;
        this.smoothedFps = 0;
        this.webrtcLatency = 0;
    }

    public initWebRTC() {
        if (this.statsInterval) clearInterval(this.statsInterval);
        this.canvasLoopGeneration++;

        if (this.rtcPeer) {
            this.rtcPeer.close();
        }
        
        // Always clear srcObject to ensure a fresh MediaStream is created for the new session
        if (videoEl) {
            videoEl.srcObject = null;
        }
        
        this.isWebRtcActive = false;
        this.resetStats();
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

        window.getStats = () => {
            return {
                fps: this.fps,
                bytesReceived: this.lastBytesReceived,
                latency: this.webrtcLatency,
                totalDecoded: this.lastTotalDecoded,
                webrtcFps: this.fps
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
                        const lowLatencyReceiver = receiver as LowLatencyReceiver;
                        if ('playoutDelayHint' in lowLatencyReceiver) lowLatencyReceiver.playoutDelayHint = 0;
                        if ('jitterBufferTarget' in lowLatencyReceiver) lowLatencyReceiver.jitterBufferTarget = 0;
                    });
                }
            }
        };

        this.rtcPeer.ontrack = (e: RTCTrackEvent) => {
            log('WebRTC track received: ' + e.track.kind);
            
            // Apply low-latency hints immediately to the receiver
            const lowLatencyReceiver = e.receiver as LowLatencyReceiver;
            if ('playoutDelayHint' in lowLatencyReceiver) lowLatencyReceiver.playoutDelayHint = 0;
            if ('jitterBufferTarget' in lowLatencyReceiver) lowLatencyReceiver.jitterBufferTarget = 0;

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
                this.startVideoCanvasLoop(this.canvasLoopGeneration, 0);
            }).catch((err: unknown) => {
                log('Media play error: ' + (err as Error).message);
                // Still active even if play() was interrupted, as stats will still flow
                this.startVideoCanvasLoop(this.canvasLoopGeneration, 0);
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

    private startVideoCanvasLoop = (generation: number, now: DOMHighResTimeStamp, metadata?: VideoFrameCallbackMetadata) => {
        if (generation !== this.canvasLoopGeneration || !this.isWebRtcActive) return;
        if (ctx && videoEl.videoWidth > 0) {
            this.lastCanvasFrameTime = Date.now();

            let frameMeta: PresentedFrameMeta | null = null;
            if (metadata) {
                this.frameCount++;
                
                // Expose high-precision latency metadata for benchmarks
                frameMeta = {
                    callbackAtMs: performance.timeOrigin + now,
                    presentationAtMs: performance.timeOrigin + metadata.presentationTime,
                    expectedDisplayAtMs: performance.timeOrigin + metadata.expectedDisplayTime,
                    captureAtMs: performance.timeOrigin + ((metadata as VideoFrameCallbackMetadata & { captureTime?: number }).captureTime || 0),
                    receiveAtMs: performance.timeOrigin + ((metadata as VideoFrameCallbackMetadata & { receiveTime?: number }).receiveTime || metadata.presentationTime),
                    processingDurationMs: (metadata.processingDuration || 0) * 1000,
                    presentedFrames: metadata.presentedFrames
                };
                (window as WebRTCDebugWindow).__llrdcLatestFrameMeta = frameMeta;
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
            if (frameMeta) {
                this.onPresentedFrame?.(frameMeta);
            }
            this.updateStats();
        }

        if (videoEl.requestVideoFrameCallback) {
            videoEl.requestVideoFrameCallback((nextNow, nextMetadata) => this.startVideoCanvasLoop(generation, nextNow, nextMetadata));
        } else {
            requestAnimationFrame((nextNow) => this.startVideoCanvasLoop(generation, nextNow));
        }
    }

    public pollStats() {
        if (!this.rtcPeer) {
            return;
        }
        return this.rtcPeer.getStats(null).then(stats => {
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

            (window as WebRTCDebugWindow).audioStats = {
                hasAudioTrack: hasAudioTrack,
                bytesReceived: audioBytes
            };

            // Bandwidth Calculation
            if (bytesReceived === 0) {
                stats.forEach(report => {
                    if (report.type === 'inbound-rtp' && typeof report.bytesReceived === 'number' && report.bytesReceived > 0) {
                        bytesReceived += report.bytesReceived;
                    }
                });
            }

            if (this.lastBytesReceived > 0 && bytesReceived >= this.lastBytesReceived) {
                const deltaBytes = bytesReceived - this.lastBytesReceived;
                const bits = deltaBytes * 8;
                const currentBw = bits / (deltaMs / 1000) / 1000000;
                
                if (deltaBytes > 0) {
                    this.smoothedBandwidth = (this.smoothedBandwidth * 0.8) + (currentBw * 0.2);
                } else {
                    this.smoothedBandwidth *= 0.8;
                }
            } else if (bytesReceived === 0 && this.lastBytesReceived > 0) {
                this.smoothedBandwidth *= 0.5;
            }
            this.bandwidthMbps = this.smoothedBandwidth;
            this.lastBytesReceived = bytesReceived;

            // FPS Calculation
            const isCanvasActive = (Date.now() - this.lastCanvasFrameTime) < 2000;
            let currentFps: number;
            
            let decodedFps = 0;
            let hasDecodedFps = false;
            if (framesDecoded !== -1 && this.lastTotalDecoded !== -1) {
                const decodedDelta = framesDecoded - this.lastTotalDecoded;
                decodedFps = (decodedDelta * 1000) / deltaMs;
                hasDecodedFps = true;
            }
            if (framesDecoded !== -1) {
                this.lastTotalDecoded = framesDecoded;
            }

            if (hasDecodedFps) {
                currentFps = decodedFps;
                this.frameCount = 0;
                this.lastFPSUpdate = now;
            } else if (isCanvasActive) {
                const timeSinceLastFPS = now - this.lastFPSUpdate;
                currentFps = (this.frameCount * 1000) / timeSinceLastFPS;
                this.frameCount = 0;
                this.lastFPSUpdate = now;
            } else {
                currentFps = decodedFps;
            }

            if (this.smoothedFps === 0 && currentFps > 0) {
                this.smoothedFps = currentFps;
            } else {
                this.smoothedFps = (this.smoothedFps * 0.7) + (currentFps * 0.3);
            }
            this.fps = Math.round(this.smoothedFps);

            if (this.fps > 5 && !this.hasSentWebrtcReady && this.rtcPeer?.iceConnectionState === 'connected') {
                this.sendWs(JSON.stringify({ type: 'webrtc_ready' }));
                this.hasSentWebrtcReady = true;
            }
        }).catch(() => { });
    }

    private updateStats() {
        // UI updates unified in pollStats()
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

    public getCurrentLatency() {
        return this.webrtcLatency;
    }
}
