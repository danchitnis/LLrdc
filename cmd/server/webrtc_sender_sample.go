package main

import (
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type sampleVideoWriter struct {
	track       *webrtc.TrackLocalStaticSample
	codecFamily string
}

func newSampleVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string) (*sampleVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticSample(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &sampleVideoWriter{
		track:       track,
		codecFamily: codecFamily,
	}, nil
}

func (w *sampleVideoWriter) TrackLocal() webrtc.TrackLocal {
	return w.track
}

func (w *sampleVideoWriter) WriteFrame(frame WebRTCFrame) error {
	if err := validateFrameCodec(frame, w.codecFamily); err != nil {
		return nil
	}

	trace := frame.LatencyTrace
	if trace == nil {
		trace = startLatencyProbeFrameSend(benchmarkClockNowMs())
	} else {
		noteLatencyProbeFrameSendStart(trace, benchmarkClockNowMs())
	}
	noteLatencyProbeFirstPacketAttempt(trace, benchmarkClockNowMs())
	armLatencyProbePendingSampleTrace(trace)
	defer clearLatencyProbePendingSampleTrace(trace)
	defer finishLatencyProbeFrameSend(trace, 0)

	return w.track.WriteSample(media.Sample{
		Data:     frame.Data,
		Duration: frameDuration(),
	})
}
