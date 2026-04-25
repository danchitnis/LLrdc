package server

import (
	"testing"

	"github.com/pion/rtp"
)

type fakeH264PacketWriter struct {
	packets []*rtp.Packet
}

func (w *fakeH264PacketWriter) WriteRTP(packet *rtp.Packet) error {
	clone := &rtp.Packet{
		Header:  packet.Header,
		Payload: append([]byte(nil), packet.Payload...),
	}
	w.packets = append(w.packets, clone)
	return nil
}

func TestSplitAnnexB(t *testing.T) {
	// data: [00 00 01] [09 f0] [00] [00 00 01] [67 42 00]
	// Should return: [09 f0 00], [67 42 00]
	data := []byte{0x00, 0x00, 0x01, 0x09, 0xf0, 0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00}
	nalus := splitAnnexB(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NALUs, got %d", len(nalus))
	}
	// Note: trailing zero of next start code prefix might remain at end of previous NALU
	if nalus[0][0] != 0x09 {
		t.Fatalf("first NALU start byte = %x, want 0x09", nalus[0][0])
	}
	if nalus[1][0] != 0x67 {
		t.Fatalf("second NALU start byte = %x, want 0x67", nalus[1][0])
	}
}

func TestWriteH264NALURTPSinglePacket(t *testing.T) {
	writer := &fakeH264PacketWriter{}
	nalu := []byte{0x67, 0x42, 0x00, 0x1f}
	sequence := uint16(100)
	timestamp := uint32(1234)

	err := writeH264NALURTP(writer, nalu, timestamp, &sequence, 1200, nil, true, true)
	if err != nil {
		t.Fatalf("writeH264NALURTP returned error: %v", err)
	}

	if len(writer.packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(writer.packets))
	}
	if !writer.packets[0].Marker {
		t.Fatalf("marker should be set for single NALU frame")
	}
}

func TestWriteH264NALURTPFragmentation(t *testing.T) {
	writer := &fakeH264PacketWriter{}
	nalu := []byte{0x65, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a}
	sequence := uint16(100)
	timestamp := uint32(1234)

	err := writeH264NALURTP(writer, nalu, timestamp, &sequence, 5, nil, true, true)
	if err != nil {
		t.Fatalf("writeH264NALURTP returned error: %v", err)
	}

	if len(writer.packets) != 4 {
		t.Fatalf("expected 4 packets, got %d", len(writer.packets))
	}
}
