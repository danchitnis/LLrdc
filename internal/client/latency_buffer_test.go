package client

import (
	"io"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/transport/v4/packetio"
)

func TestLatencyBufferFactoryRecordsQueuedRTPPacket(t *testing.T) {
	t.Parallel()

	var recordedSSRC uint32
	var recordedTimestamp uint32
	var recordedSequence uint16
	var recordedAt int64

	factory := newLatencyBufferFactory(
		func() int64 { return 55 },
		func(ssrc uint32, timestamp uint32, sequence uint16, at int64) {
			recordedSSRC = ssrc
			recordedTimestamp = timestamp
			recordedSequence = sequence
			recordedAt = at
		},
	)
	buffer := factory(packetio.RTPBufferPacket, 77)

	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			SequenceNumber: 9,
			Timestamp:      777,
		},
		Payload: []byte{1, 2, 3},
	}
	raw, err := packet.Marshal()
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	if _, err := buffer.Write(raw); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	readBuf := make([]byte, 1500)
	if _, err := buffer.Read(readBuf); err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if recordedSSRC != 77 {
		t.Fatalf("recorded SSRC = %d, want 77", recordedSSRC)
	}
	if recordedTimestamp != 777 {
		t.Fatalf("recorded timestamp = %d, want 777", recordedTimestamp)
	}
	if recordedSequence != 9 {
		t.Fatalf("recorded sequence = %d, want 9", recordedSequence)
	}
	if recordedAt != 55 {
		t.Fatalf("recorded time = %d, want 55", recordedAt)
	}
}

func TestLatencyBufferFactorySkipsNonRTPBuffer(t *testing.T) {
	t.Parallel()

	called := false
	factory := newLatencyBufferFactory(
		func() int64 { return 1 },
		func(uint32, uint32, uint16, int64) { called = true },
	)
	buffer := factory(packetio.RTCPBufferPacket, 77)
	if _, err := buffer.Write([]byte{1, 2, 3}); err != nil && err != io.ErrShortWrite {
		t.Fatalf("Write returned unexpected error: %v", err)
	}
	if called {
		t.Fatal("did not expect non-RTP buffer write to be recorded")
	}
}
