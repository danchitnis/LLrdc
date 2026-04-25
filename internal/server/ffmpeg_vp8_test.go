package server

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSplitIVFEmitsEncodedFrameMetadata(t *testing.T) {
	var stream bytes.Buffer

	header := make([]byte, 32)
	copy(header[0:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(header[4:6], 0)
	binary.LittleEndian.PutUint16(header[6:8], 32)
	copy(header[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(header[12:14], 1280)
	binary.LittleEndian.PutUint16(header[14:16], 720)
	binary.LittleEndian.PutUint32(header[16:20], 60)
	binary.LittleEndian.PutUint32(header[20:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 1)
	stream.Write(header)

	frameHeader := make([]byte, 12)
	binary.LittleEndian.PutUint32(frameHeader[0:4], 3)
	binary.LittleEndian.PutUint64(frameHeader[4:12], 123)
	stream.Write(frameHeader)
	stream.Write([]byte{0x10, 0x01, 0x02})

	var frame EncodedVideoFrame
	called := false
	splitIVF(bytes.NewReader(stream.Bytes()), func(parsed EncodedVideoFrame) {
		if called {
			t.Fatalf("unexpected second frame callback: %+v", parsed)
		}
		called = true
		frame = parsed
	})

	if !called {
		t.Fatal("expected splitIVF callback to be invoked")
	}
	if got, want := frame.Data, []byte{0x10, 0x01, 0x02}; !bytes.Equal(got, want) {
		t.Fatalf("unexpected frame data: got %#v want %#v", got, want)
	}
	if got, want := frame.ContainerTimestamp, uint64(123); got != want {
		t.Fatalf("unexpected container timestamp: got %d want %d", got, want)
	}
	if frame.ParsedAtMs <= 0 {
		t.Fatalf("expected ParsedAtMs to be set, got %d", frame.ParsedAtMs)
	}
}
