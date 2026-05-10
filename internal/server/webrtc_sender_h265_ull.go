package server

import (
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type h265RTPPacketWriter interface {
	WriteRTP(packet *rtp.Packet) error
}

type h265ULLVideoWriter struct {
	track       *webrtc.TrackLocalStaticRTP
	codecFamily string

	mu           sync.Mutex
	sequence     uint16
	timestampOffset uint32
	initialized  bool
	maxFramePart int
}

func newH265ULLVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string) (*h265ULLVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &h265ULLVideoWriter{
		track:        track,
		codecFamily:  codecFamily,
		maxFramePart: webrtcVideoOutboundMTU - 12,
	}, nil
}

func (w *h265ULLVideoWriter) TrackLocal() webrtc.TrackLocal {
	return w.track
}

func (w *h265ULLVideoWriter) WriteFrame(frame WebRTCFrame) error {
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

	baseTimestamp := uint32(frame.CaptureTime.UnixNano() * 90000 / 1e9)
	if !w.initialized {
		w.sequence = cryptoRandomUint16()
		w.timestampOffset = cryptoRandomUint32() - baseTimestamp
		w.initialized = true
	}
	if w.maxFramePart <= 3 {
		w.maxFramePart = 4
	}

	currentTimestamp := baseTimestamp + w.timestampOffset

	nalus := splitAnnexB(frame.Data)
	var filteredNALUs [][]byte
	for _, nalu := range nalus {
		if len(nalu) < 2 {
			continue
		}
		naluType := (nalu[0] >> 1) & 0x3f
		if naluType == 35 { // Skip Access Unit Delimiter (AUD)
			continue
		}
		filteredNALUs = append(filteredNALUs, nalu)
	}

	var err error
	sentFirst := false
	for i, nalu := range filteredNALUs {
		isLastNALU := (i == len(filteredNALUs)-1)
		isFirst := !sentFirst

		err = writeH265NALURTP(w.track, nalu, currentTimestamp, &w.sequence, w.maxFramePart, trace, isFirst, isLastNALU)
		if err == nil {
			sentFirst = true
		} else {
			break
		}
	}

	
	if err != nil {
		finishLatencyProbeFrameSend(trace, 0)
		return err
	}

	finishLatencyProbeFrameSend(trace, 0)
	return nil
}

func writeH265NALURTP(writer h265RTPPacketWriter, nalu []byte, timestamp uint32, sequence *uint16, maxFragmentSize int, trace *latencyProbeSendTrace, isFirst bool, isLast bool) error {
	if len(nalu) < 2 {
		return nil
	}

	if len(nalu) <= maxFragmentSize {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         isLast,
			},
			Payload: nalu,
		}

		now := benchmarkClockNowMs()
		if isFirst {
			noteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			noteLatencyProbeFirstPacketAttempt(trace, now)
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if isFirst {
			noteLatencyProbeFirstPacket(trace, now)
		}
		if isLast {
			noteLatencyProbeLastPacket(trace, now)
		}
		(*sequence)++
		return nil
	}

	// FU-A Fragmentation
	h0 := nalu[0]
	h1 := nalu[1]
	naluType := (h0 >> 1) & 0x3f
	payload := nalu[2:]

	firstFragment := true
	for len(payload) > 0 {
		chunkSize := maxFragmentSize - 3 // 2 bytes NAL header + 1 byte FU header
		if chunkSize > len(payload) {
			chunkSize = len(payload)
		}
		chunk := payload[:chunkSize]
		payload = payload[chunkSize:]

		// FU-A Indicator NAL header (Type 49)
		fuIndicatorH0 := (h0 & 0x81) | (49 << 1)
		fuIndicatorH1 := h1

		// FU Header
		fuHeader := naluType
		if firstFragment {
			fuHeader |= 0x80 // Start bit
		}
		if len(payload) == 0 {
			fuHeader |= 0x40 // End bit
		}

		rtpPayload := make([]byte, 3+len(chunk))
		rtpPayload[0] = fuIndicatorH0
		rtpPayload[1] = fuIndicatorH1
		rtpPayload[2] = fuHeader
		copy(rtpPayload[3:], chunk)

		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				SequenceNumber: *sequence,
				Timestamp:      timestamp,
				Marker:         isLast && len(payload) == 0,
			},
			Payload: rtpPayload,
		}

		now := benchmarkClockNowMs()
		if isFirst && firstFragment {
			noteLatencyProbeFirstPacketIdentity(trace, packet.SequenceNumber, packet.Timestamp)
			noteLatencyProbeFirstPacketAttempt(trace, now)
		}
		if err := writer.WriteRTP(packet); err != nil {
			return err
		}
		if isFirst && firstFragment {
			noteLatencyProbeFirstPacket(trace, now)
		}
		if isLast && len(payload) == 0 {
			noteLatencyProbeLastPacket(trace, now)
		}
		(*sequence)++
		firstFragment = false
	}

	return nil
}
