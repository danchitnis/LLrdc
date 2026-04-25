package main

import (
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const vp8PayloadDescriptorSize = 1

type vp8RTPPacketWriter interface {
	WriteRTP(packet *rtp.Packet) error
}

type vp8ULLVideoWriter struct {
	track       *webrtc.TrackLocalStaticRTP
	codecFamily string

	mu           sync.Mutex
	sequence     uint16
	timestamp    uint32
	frameStep    uint32
	initialized  bool
	maxFramePart int
}

func newVP8ULLVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string) (*vp8ULLVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &vp8ULLVideoWriter{
		track:        track,
		codecFamily:  codecFamily,
		frameStep:    frameSamples(),
		maxFramePart: webrtcVideoOutboundMTU - 12 - vp8PayloadDescriptorSize,
	}, nil
}

func (w *vp8ULLVideoWriter) TrackLocal() webrtc.TrackLocal {
	return w.track
}

func (w *vp8ULLVideoWriter) WriteFrame(frame WebRTCFrame) error {
	if err := validateFrameCodec(frame, w.codecFamily); err != nil {
		return nil
	}
	if len(frame.Data) == 0 {
		return nil
	}

	trace := frame.LatencyTrace
	if trace == nil {
		trace = startLatencyProbeFrameSend(benchmarkClockNowMs())
	} else {
		noteLatencyProbeFrameSendStart(trace, benchmarkClockNowMs())
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized {
		w.sequence = cryptoRandomUint16()
		w.timestamp = cryptoRandomUint32()
		w.initialized = true
	}
	if w.maxFramePart <= 0 {
		w.maxFramePart = 1
	}

	err := writeVP8FrameRTP(w.track, frame.Data, w.timestamp, &w.sequence, w.maxFramePart, trace)
	w.timestamp += w.frameStep
	if err != nil {
		finishLatencyProbeFrameSend(trace, 0)
		return err
	}

	finishLatencyProbeFrameSend(trace, 0)
	return nil
}

func writeVP8FrameRTP(writer vp8RTPPacketWriter, frame []byte, timestamp uint32, sequence *uint16, maxFragmentSize int, trace *latencyProbeSendTrace) error {
	remaining := frame
	firstFragment := true

	for len(remaining) > 0 {
		chunkSize := maxFragmentSize
		if chunkSize > len(remaining) {
			chunkSize = len(remaining)
		}
		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		payload := make([]byte, vp8PayloadDescriptorSize+len(chunk))
		if firstFragment {
			payload[0] = 0x10
		}
		copy(payload[vp8PayloadDescriptorSize:], chunk)

		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    0,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         len(remaining) == 0,
			},
			Payload: payload,
		}

		if firstFragment {
			noteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			noteLatencyProbeFirstPacketAttempt(trace, benchmarkClockNowMs())
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if firstFragment {
			noteLatencyProbeFirstPacket(trace, benchmarkClockNowMs())
		}
		(*sequence)++
		firstFragment = false
	}

	noteLatencyProbeLastPacket(trace, benchmarkClockNowMs())
	return nil
}
