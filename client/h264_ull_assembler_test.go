package client

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
)

func TestH264ULLAssemblerMultiNALU(t *testing.T) {
	a := newH264ULLAssembler()
	timing := packetTiming{}

	// SPS (Single NALU, type 7)
	p1 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 1,
			PayloadType:    96,
		},
		Payload: []byte{0x07, 0x01, 0x02, 0x03},
	}

	// PPS (Single NALU, type 8)
	p2 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 2,
			PayloadType:    96,
		},
		Payload: []byte{0x08, 0x04, 0x05},
	}

	// IDR Slice (FU-A Start, type 28, S=1, nalType=5)
	p3 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 3,
			PayloadType:    96,
			Marker:         true,
		},
		Payload: []byte{0x1C, 0x85, 0x06, 0x07},
	}

	frame, ready, dropped, err := a.push(p1, timing, 0)
	if err != nil || ready || dropped {
		t.Fatalf("p1 failed: err=%v, ready=%v, dropped=%v", err, ready, dropped)
	}

	frame, ready, dropped, err = a.push(p2, timing, 0)
	if err != nil || ready || dropped {
		t.Fatalf("p2 failed: err=%v, ready=%v, dropped=%v", err, ready, dropped)
	}

	frame, ready, dropped, err = a.push(p3, timing, 0)
	if err != nil || !ready || dropped {
		t.Fatalf("p3 failed: err=%v, ready=%v, dropped=%v", err, ready, dropped)
	}

	// Expected: Annex B with start codes for EACH NALU
	// 00 00 00 01 07 01 02 03
	// 00 00 00 01 08 04 05
	// 00 00 00 01 05 06 07
	expected := []byte{
		0, 0, 0, 1, 0x07, 0x01, 0x02, 0x03,
		0, 0, 0, 1, 0x08, 0x04, 0x05,
		0, 0, 0, 1, 0x05, 0x06, 0x07,
	}

	if !bytes.Equal(frame.data, expected) {
		t.Errorf("frame data mismatch.\ngot:  %x\nwant: %x", frame.data, expected)
	}
}

func TestH264ULLAssemblerFUAFragmentation(t *testing.T) {
	a := newH264ULLAssembler()
	timing := packetTiming{}

	// FU-A Start (type 28, S=1, nalType=5)
	p1 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 1,
			PayloadType:    96,
		},
		Payload: []byte{0x1C, 0x85, 0x01, 0x02},
	}

	// FU-A Mid (type 28, S=0, E=0, nalType=5)
	p2 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 2,
			PayloadType:    96,
		},
		Payload: []byte{0x1C, 0x05, 0x03, 0x04},
	}

	// FU-A End (type 28, S=0, E=1, nalType=5)
	p3 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 3,
			PayloadType:    96,
			Marker:         true,
		},
		Payload: []byte{0x1C, 0x45, 0x05, 0x06},
	}

	a.push(p1, timing, 0)
	a.push(p2, timing, 0)
	frame, ready, _, _ := a.push(p3, timing, 0)

	if !ready {
		t.Fatal("frame not ready")
	}

	// Expected: Annex B with ONE start code for the whole FU-A reassembled NALU
	// 00 00 00 01 05 01 02 03 04 05 06
	expected := []byte{
		0, 0, 0, 1, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
	}

	if !bytes.Equal(frame.data, expected) {
		t.Errorf("frame data mismatch.\ngot:  %x\nwant: %x", frame.data, expected)
	}
}

func TestH264ULLAssemblerRobustness(t *testing.T) {
	a := newH264ULLAssembler()
	timing := packetTiming{}

	// Packet 1: FU-A Continuation (NOT a start)
	p1 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 1,
			PayloadType:    96,
		},
		Payload: []byte{0x1C, 0x05, 0x01, 0x02},
	}

	frame, ready, _, _ := a.push(p1, timing, 0)
	if ready || a.active {
		t.Fatal("assembler should not start on a middle fragment")
	}

	// Packet 2: Single NALU (Valid start)
	p2 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 2,
			PayloadType:    96,
			Marker:         true,
		},
		Payload: []byte{0x07, 0x01, 0x02},
	}

	frame, ready, _, _ = a.push(p2, timing, 0)
	if !ready || !bytes.Equal(frame.data, []byte{0, 0, 0, 1, 0x07, 0x01, 0x02}) {
		t.Fatalf("assembler failed to recover on valid boundary: ready=%v, data=%x", ready, frame.data)
	}
}

func TestH264ULLAssemblerSTAPA(t *testing.T) {
	a := newH264ULLAssembler()
	timing := packetTiming{}

	// STAP-A (type 24) containing SPS and PPS
	// Payload: [24] [size1:2] [NAL1] [size2:2] [NAL2]
	p1 := &rtp.Packet{
		Header: rtp.Header{
			Timestamp:      100,
			SequenceNumber: 1,
			PayloadType:    96,
			Marker:         true,
		},
		Payload: []byte{
			0x18,
			0x00, 0x02, 0x07, 0x01,
			0x00, 0x02, 0x08, 0x02,
		},
	}

	frame, ready, _, _ := a.push(p1, timing, 0)
	if !ready {
		t.Fatal("frame not ready")
	}

	// Pion's H264 Unmarshal for STAP-A returns Annex B: 00 00 00 01 NAL1 00 00 00 01 NAL2
	// Our code should NOT prepend another 00 00 00 01 if it's already there.
	expected := []byte{
		0, 0, 0, 1, 0x07, 0x01,
		0, 0, 0, 1, 0x08, 0x02,
	}

	if !bytes.Equal(frame.data, expected) {
		t.Errorf("frame data mismatch.\ngot:  %x\nwant: %x", frame.data, expected)
	}
}
