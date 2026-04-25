package client

import (
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

func (a *h264ULLAssembler) start(packet *rtp.Packet, timing packetTiming, firstPacketReadAt int64, payload []byte) {
	a.active = true
	a.timestamp = packet.Timestamp
	a.nextSequence = packet.SequenceNumber + 1
	a.firstPacketSequenceNumber = packet.SequenceNumber
	a.firstDecryptedPacketQueuedAt = timing.firstDecryptedPacketQueuedAt
	a.firstRemotePacketAt = timing.firstRemotePacketAt
	a.firstPacketReadAt = firstPacketReadAt

	// Start with Annex B start code
	a.frame = append(a.frame[:0], 0, 0, 0, 1)
	a.frame = append(a.frame, payload...)
}

func (a *h264ULLAssembler) push(packet *rtp.Packet, timing packetTiming, packetReadAt int64) (frame h264ULLFrame, ready bool, dropped bool, err error) {
	if a.active && (packet.Timestamp != a.timestamp || packet.SequenceNumber != a.nextSequence) {
		dropped = len(a.frame) > 0
		a.reset()
	}

	payload, err := a.packetizer.Unmarshal(packet.Payload)
	if err != nil {
		a.reset()
		return frame, false, true, err
	}

	if !a.active {
		a.start(packet, timing, packetReadAt, payload)
	} else {
		a.nextSequence = packet.SequenceNumber + 1
		
		// If this is a new NALU with the same timestamp (STAP-A or just multiple packets),
		// we should ideally detect it. But since we split them in the server,
		// we know that if it's NOT an FU-A fragment (or if it's the start of an FU-A),
		// it's a new NALU.
		// Pion's Unmarshal returns the NALU header for the first fragment/single packet.
		// We can detect new NALUs by checking if it's not a mid-fragment.
		
		isNewNALU := true
		if len(packet.Payload) > 0 {
			naluType := packet.Payload[0] & 0x1f
			if naluType == 28 { // FU-A
				fuHeader := packet.Payload[1]
				isStart := (fuHeader & 0x80) != 0
				isNewNALU = isStart
			}
		}

		if isNewNALU {
			a.frame = append(a.frame, 0, 0, 0, 1)
		}
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
