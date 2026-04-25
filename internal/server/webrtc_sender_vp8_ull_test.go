package server

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
)

type fakeVP8PacketWriter struct {
	packets []*rtp.Packet
}

func (w *fakeVP8PacketWriter) WriteRTP(packet *rtp.Packet) error {
	clone := &rtp.Packet{
		Header:  packet.Header,
		Payload: append([]byte(nil), packet.Payload...),
	}
	w.packets = append(w.packets, clone)
	return nil
}

func TestWriteVP8FrameRTPPacketizesFrame(t *testing.T) {
	writer := &fakeVP8PacketWriter{}
	frame := []byte("abcdefghijklmnopqrstuvwxyz")
	sequence := uint16(100)
	timestamp := uint32(1234)

	if err := writeVP8FrameRTP(writer, frame, timestamp, &sequence, 10, nil); err != nil {
		t.Fatalf("writeVP8FrameRTP returned error: %v", err)
	}

	if len(writer.packets) != 3 {
		t.Fatalf("expected 3 RTP packets, got %d", len(writer.packets))
	}

	var rebuilt []byte
	for i, packet := range writer.packets {
		if packet.Timestamp != timestamp {
			t.Fatalf("packet %d timestamp = %d, want %d", i, packet.Timestamp, timestamp)
		}
		if got, want := packet.SequenceNumber, uint16(100+i); got != want {
			t.Fatalf("packet %d sequence = %d, want %d", i, got, want)
		}
		if i == 0 {
			if packet.Payload[0] != 0x10 {
				t.Fatalf("first packet VP8 descriptor = %#x, want 0x10", packet.Payload[0])
			}
		} else if packet.Payload[0] != 0x00 {
			t.Fatalf("non-first packet VP8 descriptor = %#x, want 0x00", packet.Payload[0])
		}
		if got, want := packet.Marker, i == len(writer.packets)-1; got != want {
			t.Fatalf("packet %d marker = %v, want %v", i, got, want)
		}
		rebuilt = append(rebuilt, packet.Payload[vp8PayloadDescriptorSize:]...)
	}

	if !bytes.Equal(rebuilt, frame) {
		t.Fatalf("rebuilt VP8 frame mismatch: got %q want %q", rebuilt, frame)
	}
	if got, want := sequence, uint16(103); got != want {
		t.Fatalf("final sequence = %d, want %d", got, want)
	}
}

func TestWriteVP8FrameRTPSinglePacketFrame(t *testing.T) {
	writer := &fakeVP8PacketWriter{}
	sequence := uint16(7)

	if err := writeVP8FrameRTP(writer, []byte("hello"), 55, &sequence, 16, nil); err != nil {
		t.Fatalf("writeVP8FrameRTP returned error: %v", err)
	}

	if len(writer.packets) != 1 {
		t.Fatalf("expected 1 RTP packet, got %d", len(writer.packets))
	}
	if !writer.packets[0].Marker {
		t.Fatalf("single packet frame must set RTP marker")
	}
	if writer.packets[0].Payload[0] != 0x10 {
		t.Fatalf("single packet VP8 descriptor = %#x, want 0x10", writer.packets[0].Payload[0])
	}
}
