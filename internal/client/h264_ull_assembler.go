package client

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

type h264ULLFrame struct {
	data                         []byte
	packetTimestamp              uint32
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
}

type h264ULLAssembler struct {
	active                       bool
	timestamp                    uint32
	nextSequence                 uint16
	firstPacketSequenceNumber    uint16
	firstDecryptedPacketQueuedAt int64
	firstRemotePacketAt          int64
	firstPacketReadAt            int64
	frame                        []byte
	packetizer                   *codecs.H264Packet
}

func newH264ULLAssembler() *h264ULLAssembler {
	return &h264ULLAssembler{
		packetizer: &codecs.H264Packet{},
	}
}

func (a *h264ULLAssembler) reset() {
	a.active = false
	a.timestamp = 0
	a.nextSequence = 0
	a.firstPacketSequenceNumber = 0
	a.firstDecryptedPacketQueuedAt = 0
	a.firstRemotePacketAt = 0
	a.firstPacketReadAt = 0
	a.frame = nil
}

func (a *h264ULLAssembler) push(packet *rtp.Packet, timing packetTiming, packetReadAt int64) (frame h264ULLFrame, ready bool, dropped bool, err error) {
	if a.active && (packet.Timestamp != a.timestamp || packet.SequenceNumber != a.nextSequence) {
		dropped = len(a.frame) > 0
		a.reset()
	}

	if len(packet.Payload) < 1 {
		a.reset()
		return frame, false, true, fmt.Errorf("h264 RTP payload empty")
	}

	payload := packet.Payload
	naluType := payload[0] & 0x1F

	if !a.active {
		// Only start if this is a valid NALU boundary.
		// Valid starts: Single NALU (1-23), STAP-A (24), or FU-A Start (28 with S=1).
		canStart := false
		if naluType > 0 && naluType < 24 {
			canStart = true
		} else if naluType == 24 {
			canStart = true
		} else if naluType == 28 && len(payload) >= 2 && (payload[1]&0x80) != 0 {
			canStart = true
		}

		if !canStart {
			return frame, false, false, nil // Ignore middle-of-frame packets when not active
		}

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

	switch {
	case naluType == 28: // FU-A
		if len(payload) < 2 {
			a.reset()
			return frame, false, true, fmt.Errorf("h264 FU-A payload too short")
		}
		fuHeader := payload[1]
		isStart := (fuHeader & 0x80) != 0
		if isStart {
			a.frame = append(a.frame, 0, 0, 0, 1)
			reconstructed := (payload[0] & 0xE0) | (fuHeader & 0x1F)
			a.frame = append(a.frame, reconstructed)
			a.frame = append(a.frame, payload[2:]...)
		} else {
			a.frame = append(a.frame, payload[2:]...)
		}
	case naluType == 24: // STAP-A
		off := 1
		for off+2 <= len(payload) {
			size := int(binary.BigEndian.Uint16(payload[off : off+2]))
			off += 2
			if off+size > len(payload) {
				break
			}
			a.frame = append(a.frame, 0, 0, 0, 1)
			a.frame = append(a.frame, payload[off:off+size]...)
			off += size
		}
	default: // Single NAL Unit
		a.frame = append(a.frame, 0, 0, 0, 1)
		a.frame = append(a.frame, payload...)
	}

	if !packet.Marker {
		return frame, false, dropped, nil
	}

	frame = h264ULLFrame{
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

func shouldUseH264ULLAssembler(codecName string, lowLatency bool) bool {
	return lowLatency && strings.Contains(strings.ToLower(codecName), "h264")
}
