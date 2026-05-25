package common

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
	payloadType uint8

	mu              sync.Mutex
	sequence        uint16
	timestampOffset uint32
	initialized     bool
	maxFramePart    int
}

func newVP8ULLVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string, payloadType uint8) (*vp8ULLVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &vp8ULLVideoWriter{
		track:        track,
		codecFamily:  codecFamily,
		payloadType:  payloadType,
		maxFramePart: webrtcVideoOutboundMTU - 12,
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
		trace = StartLatencyProbeFrameSend(BenchmarkClockNowMs())
	} else {
		NoteLatencyProbeFrameSendStart(trace, BenchmarkClockNowMs())
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	baseTimestamp := uint32(frame.CaptureTime.UnixNano() * 90000 / 1e9)
	if !w.initialized {
		w.sequence = cryptoRandomUint16()
		w.timestampOffset = cryptoRandomUint32() - baseTimestamp
		w.initialized = true
	}
	if w.maxFramePart <= 0 {
		w.maxFramePart = 1
	}

	currentTimestamp := baseTimestamp + w.timestampOffset
	err := writeVP8FrameRTP(w.track, frame.Data, w.payloadType, currentTimestamp, &w.sequence, w.maxFramePart, trace)
	if err != nil {
		FinishLatencyProbeFrameSend(trace, 0)
		return err
	}

	FinishLatencyProbeFrameSend(trace, 0)
	return nil
}

func writeVP8FrameRTP(writer vp8RTPPacketWriter, frame []byte, payloadType uint8, timestamp uint32, sequence *uint16, maxFragmentSize int, trace *LatencyProbeSendTrace) error {
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
				PayloadType:    payloadType,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         len(remaining) == 0,
			},
			Payload: payload,
		}

		if firstFragment {
			NoteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			NoteLatencyProbeFirstPacketAttempt(trace, BenchmarkClockNowMs())
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if firstFragment {
			NoteLatencyProbeFirstPacket(trace, BenchmarkClockNowMs())
		}
		(*sequence)++
		firstFragment = false
	}

	NoteLatencyProbeLastPacket(trace, BenchmarkClockNowMs())
	return nil
}
