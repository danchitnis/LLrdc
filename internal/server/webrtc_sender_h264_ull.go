package server

import (
	"bytes"
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

	mu           sync.Mutex
	sequence     uint16
	timestamp    uint32
	frameStep    uint32
	initialized  bool
	maxFramePart int
}

func newH264ULLVideoWriter(capability webrtc.RTPCodecCapability, codecFamily string) (*h264ULLVideoWriter, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(capability, "video", "pion")
	if err != nil {
		return nil, err
	}
	return &h264ULLVideoWriter{
		track:        track,
		codecFamily:  codecFamily,
		frameStep:    frameSamples(),
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
	if w.maxFramePart <= 2 {
		w.maxFramePart = 3
	}

	nalus := splitAnnexB(frame.Data)
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

		err = writeH264NALURTP(w.track, nalu, w.timestamp, &w.sequence, w.maxFramePart, trace, isFirst, isLastNALU)
		if err == nil {
			sentFirst = true
		} else {
			break
		}
	}

	w.timestamp += w.frameStep
	if err != nil {
		finishLatencyProbeFrameSend(trace, 0)
		return err
	}

	finishLatencyProbeFrameSend(trace, 0)
	return nil
}

func splitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	parts := bytes.Split(data, []byte{0, 0, 1})
	for _, p := range parts {
		// Trim leading zeros which are part of the start code prefix (00 00 00 01)
		for len(p) > 0 && p[0] == 0 {
			p = p[1:]
		}
		if len(p) > 0 {
			// Preserving trailing data is safer for decoders
			nalus = append(nalus, p)
		}
	}
	return nalus
}

func writeH264NALURTP(writer h264RTPPacketWriter, nalu []byte, timestamp uint32, sequence *uint16, maxFragmentSize int, trace *latencyProbeSendTrace, isFirst bool, isLast bool) error {
	if len(nalu) == 0 {
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
