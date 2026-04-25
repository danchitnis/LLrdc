package main

import "testing"

func TestNewVideoFrameWriterSelectsULLWriter(t *testing.T) {
	cases := []struct {
		name  string
		codec string
	}{
		{name: "vp8 ull", codec: "vp8"},
		{name: "h264 ull", codec: "h264"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer, err := newVideoFrameWriter(tc.codec, true)
			if err != nil {
				t.Fatalf("newVideoFrameWriter returned error: %v", err)
			}

			switch tc.codec {
			case "vp8":
				if _, ok := writer.(*vp8ULLVideoWriter); !ok {
					t.Fatalf("expected vp8ULLVideoWriter, got %T", writer)
				}
			case "h264":
				if _, ok := writer.(*h264ULLVideoWriter); !ok {
					t.Fatalf("expected h264ULLVideoWriter, got %T", writer)
				}
			}
		})
	}
}

func TestNewVideoFrameWriterSelectsSampleWriter(t *testing.T) {
	cases := []struct {
		name       string
		codec      string
		lowLatency bool
	}{
		{name: "vp8 normal", codec: "vp8", lowLatency: false},
		{name: "h264 normal", codec: "h264", lowLatency: false},
		{name: "av1 ull", codec: "av1", lowLatency: true},
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
