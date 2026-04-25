package client

import (
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

func TestRemotePacketTimestampInterceptorRecordsFirstVideoPacket(t *testing.T) {
	t.Parallel()

	var recordedSSRC uint32
	var recordedTimestamp uint32
	var recordedSequence uint16
	var recordedAt int64

	factory := newRemotePacketTimestampInterceptorFactory(
		func() int64 { return 1234 },
		func(ssrc uint32, timestamp uint32, sequence uint16, at int64) {
			recordedSSRC = ssrc
			recordedTimestamp = timestamp
			recordedSequence = sequence
			recordedAt = at
		},
	)
	raw, err := factory.NewInterceptor("pc")
	if err != nil {
		t.Fatalf("NewInterceptor returned error: %v", err)
	}
	reader := raw.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:     42,
		MimeType: "video/VP8",
	}, interceptor.RTPReaderFunc(func(buf []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				SequenceNumber: 9,
				Timestamp:      777,
			},
			Payload: []byte{0x10, 0x01},
		}
		rawPacket, marshalErr := packet.Marshal()
		if marshalErr != nil {
			t.Fatalf("Marshal returned error: %v", marshalErr)
		}
		copy(buf, rawPacket)
		return len(rawPacket), a, nil
	}))

	buf := make([]byte, 1500)
	if _, _, err := reader.Read(buf, make(interceptor.Attributes)); err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if recordedSSRC != 42 {
		t.Fatalf("recorded SSRC = %d, want 42", recordedSSRC)
	}
	if recordedTimestamp != 777 {
		t.Fatalf("recorded timestamp = %d, want 777", recordedTimestamp)
	}
	if recordedSequence != 9 {
		t.Fatalf("recorded sequence = %d, want 9", recordedSequence)
	}
	if recordedAt != 1234 {
		t.Fatalf("recorded time = %d, want 1234", recordedAt)
	}
}

func TestRemotePacketTimestampInterceptorSkipsNonVideo(t *testing.T) {
	t.Parallel()

	called := false
	factory := newRemotePacketTimestampInterceptorFactory(
		func() int64 { return 1 },
		func(uint32, uint32, uint16, int64) { called = true },
	)
	raw, err := factory.NewInterceptor("pc")
	if err != nil {
		t.Fatalf("NewInterceptor returned error: %v", err)
	}
	reader := raw.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:     42,
		MimeType: "audio/opus",
	}, interceptor.RTPReaderFunc(func(_ []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return 0, a, nil
	}))

	if _, _, err := reader.Read(make([]byte, 4), make(interceptor.Attributes)); err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if called {
		t.Fatal("did not expect non-video stream to be recorded")
	}
}
