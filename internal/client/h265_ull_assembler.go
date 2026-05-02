package client

import (
	"fmt"
	"strings"

	"github.com/pion/rtp"
)

type h265ULLFrame struct {
	data                         []byte
	packetTimestamp              uint32
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
}

type h265ULLAssembler struct {
	active                       bool
	timestamp                    uint32
	nextSequence                 uint16
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	frame                        []byte
}

func newH265ULLAssembler() *h265ULLAssembler {
	return &h265ULLAssembler{}
}

func (a *h265ULLAssembler) reset() {
	a.active = false
	a.timestamp = 0
	a.nextSequence = 0
	a.firstPacketSequenceNumber = 0
	a.firstDecryptedPacketQueuedAt = 0
	a.firstRemotePacketAt = 0
	a.firstPacketReadAt = 0
	a.frame = nil
}

func (a *h265ULLAssembler) push(packet *rtp.Packet, timing packetTiming, packetReadAt int64) (frame h265ULLFrame, ready bool, dropped bool, err error) {
	if a.active && (packet.Timestamp != a.timestamp || packet.SequenceNumber != a.nextSequence) {
		dropped = len(a.frame) > 0
		a.reset()
	}

	payload := packet.Payload
	if len(payload) < 2 {
		a.reset()
		return frame, false, true, fmt.Errorf("h265 RTP payload too short: %d bytes", len(payload))
	}

	// Parse the HEVC RTP NAL unit header (2 bytes per RFC 7798)
	naluType := (payload[0] >> 1) & 0x3f

	switch {
	case naluType == 49: // Fragmentation Unit (FU)
		if len(payload) < 3 {
			a.reset()
			return frame, false, true, fmt.Errorf("h265 FU packet too short: %d bytes", len(payload))
		}
		fuHeader := payload[2]
		isStart := (fuHeader & 0x80) != 0
		fuPayload := payload[3:] // FU payload starts after 2-byte NAL header + 1-byte FU header

		if isStart {
			// Reconstruct the original 2-byte NAL unit header from the
			// FU indicator (payload[0:2]) and FU header (payload[2]).
			// The real NAL type is in FU header bits [5:0].
			// The F, LayerID, and TID fields come from the FU indicator.
			realType := fuHeader & 0x3f
			h0 := (payload[0] & 0x81) | (realType << 1)
			h1 := payload[1]

			if !a.active {
				a.active = true
				a.timestamp = packet.Timestamp
				a.nextSequence = packet.SequenceNumber + 1
				a.firstPacketSequenceNumber = packet.SequenceNumber
				a.firstDecryptedPacketQueuedAt = timing.firstDecryptedPacketQueuedAt
				a.firstRemotePacketAt = timing.firstRemotePacketAt
				a.firstPacketReadAt = packetReadAt
				a.frame = append(a.frame[:0], 0, 0, 0, 1) // Annex B start code
			} else {
				a.nextSequence = packet.SequenceNumber + 1
				a.frame = append(a.frame, 0, 0, 0, 1) // Annex B start code for new NAL in same AU
			}
			a.frame = append(a.frame, h0, h1)
			a.frame = append(a.frame, fuPayload...)
		} else {
			// Continuation or end fragment — just append the raw payload
			if !a.active {
				return frame, false, dropped, fmt.Errorf("h265 low-latency frame missing start fragment")
			}
			a.nextSequence = packet.SequenceNumber + 1
			a.frame = append(a.frame, fuPayload...)
		}

	case naluType == 48: // Aggregation Packet (AP)
		// AP packets contain multiple NAL units; extract each one
		if !a.active {
			a.active = true
			a.timestamp = packet.Timestamp
			a.nextSequence = packet.SequenceNumber + 1
			a.firstPacketSequenceNumber = packet.SequenceNumber
			a.firstDecryptedPacketQueuedAt = timing.firstDecryptedPacketQueuedAt
			a.firstRemotePacketAt = timing.firstRemotePacketAt
			a.firstPacketReadAt = packetReadAt
			a.frame = a.frame[:0]
		} else {
			a.nextSequence = packet.SequenceNumber + 1
		}

		// Skip the 2-byte AP header, then iterate length-prefixed NAL units
		apPayload := payload[2:]
		for len(apPayload) >= 2 {
			nalSize := int(uint16(apPayload[0])<<8 | uint16(apPayload[1]))
			apPayload = apPayload[2:]
			if nalSize > len(apPayload) {
				break
			}
			a.frame = append(a.frame, 0, 0, 0, 1) // Annex B start code
			a.frame = append(a.frame, apPayload[:nalSize]...)
			apPayload = apPayload[nalSize:]
		}

	default: // Single NAL Unit Packet
		if !a.active {
			a.active = true
			a.timestamp = packet.Timestamp
			a.nextSequence = packet.SequenceNumber + 1
			a.firstPacketSequenceNumber = packet.SequenceNumber
			a.firstDecryptedPacketQueuedAt = timing.firstDecryptedPacketQueuedAt
			a.firstRemotePacketAt = timing.firstRemotePacketAt
			a.firstPacketReadAt = packetReadAt
			a.frame = append(a.frame[:0], 0, 0, 0, 1) // Annex B start code
		} else {
			a.nextSequence = packet.SequenceNumber + 1
			a.frame = append(a.frame, 0, 0, 0, 1) // Annex B start code
		}
		// The entire RTP payload IS the NAL unit (header + body)
		a.frame = append(a.frame, payload...)
	}

	if !packet.Marker {
		return frame, false, dropped, nil
	}

	frame = h265ULLFrame{
		data:                         append([]byte(nil), a.frame...),
		packetTimestamp:              a.timestamp,
		firstPacketSequenceNumber:    a.firstPacketSequenceNumber,
		firstDecryptedPacketQueuedAt: a.firstDecryptedPacketQueuedAt,
		firstRemotePacketAt:          a.firstRemotePacketAt,
		firstPacketReadAt:            a.firstPacketReadAt,
	}
	a.reset()
	return frame, true, dropped, nil
}

func shouldUseH265ULLAssembler(codecName string, lowLatency bool) bool {
	lower := strings.ToLower(codecName)
	return lowLatency && (strings.Contains(lower, "h265") || strings.Contains(lower, "hevc"))
}
