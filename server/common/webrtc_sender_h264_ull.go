package common

import (
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type h264RTPPacketWriter interface {
	WriteRTP(packet *rtp.Packet) error
}

type h264ULLVideoWriter struct {
	track       *webrtc.TrackLocalStaticRTP
	codecFamily string
	payloadType uint8

	mu              sync.Mutex
	sequence        uint16
	timestampOffset uint32
	initialized     bool
	maxFramePart    int
}

func newH264ULLVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string, payloadType uint8) (*h264ULLVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &h264ULLVideoWriter{
		track:        track,
		codecFamily:  codecFamily,
		payloadType:  payloadType,
		maxFramePart: webrtcVideoOutboundMTU - 12,
	}, nil
}

func (w *h264ULLVideoWriter) TrackLocal() webrtc.TrackLocal {
	return w.track
}

func (w *h264ULLVideoWriter) WriteFrame(frame WebRTCFrame) error {
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
	if w.maxFramePart <= 2 {
		w.maxFramePart = 3
	}

	currentTimestamp := baseTimestamp + w.timestampOffset

	nalus := SplitAnnexB(frame.Data)
	var filteredNALUs [][]byte
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		naluType := nalu[0] & 0x1f
		if naluType == 9 { // Skip Access Unit Delimiter
			continue
		}
		filteredNALUs = append(filteredNALUs, nalu)
	}

	var err error
	sentFirst := false
	for i, nalu := range filteredNALUs {
		isLastNALU := (i == len(filteredNALUs)-1)
		isFirst := !sentFirst

		err = writeH264NALURTP(w.track, nalu, w.payloadType, currentTimestamp, &w.sequence, w.maxFramePart, trace, isFirst, isLastNALU)
		if err == nil {
			sentFirst = true
		} else {
			break
		}
	}

	if err != nil {
		FinishLatencyProbeFrameSend(trace, 0)
		return err
	}

	FinishLatencyProbeFrameSend(trace, 0)
	return nil
}

func writeH264NALURTP(writer h264RTPPacketWriter, nalu []byte, payloadType uint8, timestamp uint32, sequence *uint16, maxFragmentSize int, trace *LatencyProbeSendTrace, isFirst bool, isLast bool) error {
	if len(nalu) == 0 {
		return nil
	}

	if len(nalu) <= maxFragmentSize {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    payloadType,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         isLast,
			},
			Payload: nalu,
		}

		now := BenchmarkClockNowMs()
		if isFirst {
			NoteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			NoteLatencyProbeFirstPacketAttempt(trace, now)
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if isFirst {
			NoteLatencyProbeFirstPacket(trace, now)
		}
		if isLast {
			NoteLatencyProbeLastPacket(trace, now)
		}
		(*sequence)++
		return nil
	}

	// FU-A Fragmentation
	header := nalu[0]
	f := header & 0x80
	nri := header & 0x60
	naluType := header & 0x1f
	payload := nalu[1:]

	firstFragment := true
	for len(payload) > 0 {
		chunkSize := maxFragmentSize - 2
		if chunkSize > len(payload) {
			chunkSize = len(payload)
		}
		chunk := payload[:chunkSize]
		payload = payload[chunkSize:]

		fuIndicator := f | nri | 28
		fuHeader := naluType
		if firstFragment {
			fuHeader |= 0x80 // Start bit
		}
		if len(payload) == 0 {
			fuHeader |= 0x40 // End bit
		}

		rtpPayload := make([]byte, 2+len(chunk))
		rtpPayload[0] = fuIndicator
		rtpPayload[1] = fuHeader
		copy(rtpPayload[2:], chunk)

		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    payloadType,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         isLast && len(payload) == 0,
			},
			Payload: rtpPayload,
		}

		now := BenchmarkClockNowMs()
		if isFirst && firstFragment {
			NoteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			NoteLatencyProbeFirstPacketAttempt(trace, now)
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if isFirst && firstFragment {
			NoteLatencyProbeFirstPacket(trace, now)
		}
		if isLast && len(payload) == 0 {
			NoteLatencyProbeLastPacket(trace, now)
		}
		(*sequence)++
		firstFragment = false
	}

	return nil
}
