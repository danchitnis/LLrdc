package main

import "testing"

func TestNewVideoFrameWriterSelectsVP8ULLWriter(t *testing.T) {
	writer, err := newVideoFrameWriter("vp8", true)
	if err != nil {
		t.Fatalf("newVideoFrameWriter returned error: %v", err)
	}
	if _, ok := writer.(*vp8ULLVideoWriter); !ok {
		t.Fatalf("expected vp8ULLVideoWriter, got %T", writer)
	}
}

func TestNewVideoFrameWriterSelectsSampleWriterOutsideVP8ULL(t *testing.T) {
	cases := []struct {
		name       string
		codec      string
		lowLatency bool
	}{
		{name: "vp8 normal", codec: "vp8", lowLatency: false},
		{name: "h264 ull", codec: "h264", lowLatency: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer, err := newVideoFrameWriter(tc.codec, tc.lowLatency)
			if err != nil {
				t.Fatalf("newVideoFrameWriter returned error: %v", err)
			}
			if _, ok := writer.(*sampleVideoWriter); !ok {
				t.Fatalf("expected sampleVideoWriter, got %T", writer)
			}
		})
	}
}
