package client

import (
	"encoding/binary"
	"errors"
)

// h265AccessUnit represents a parsed HEVC access unit.
// This is primarily used for the macOS VideoToolbox renderer, which requires
// explicit parameter sets and length-prefixed NAL units (HVCC format).
type h265AccessUnit struct {
	HVCC []byte
	VPS  []byte
	SPS  []byte
	PPS  []byte
}

// buildH265AccessUnit parses an Annex-B HEVC frame and extracts the parameter sets.
// It also converts the stream to length-prefixed NALUs required by VideoToolbox.
func buildH265AccessUnit(frame []byte) (h265AccessUnit, error) {
	nalus := splitH265NALUs(frame)
	if len(nalus) == 0 {
		return h265AccessUnit{}, errors.New("h265 access unit did not contain any NAL units")
	}

	hvcc := make([]byte, 0, len(frame)+len(nalus)*4)
	unit := h265AccessUnit{}
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		naluType := (nalu[0] >> 1) & 0x3F
		switch naluType {
		case 32: // VPS
			unit.VPS = append(unit.VPS[:0], nalu...)
		case 33: // SPS
			unit.SPS = append(unit.SPS[:0], nalu...)
		case 34: // PPS
			unit.PPS = append(unit.PPS[:0], nalu...)
		}

		var sizePrefix [4]byte
		binary.BigEndian.PutUint32(sizePrefix[:], uint32(len(nalu)))
		hvcc = append(hvcc, sizePrefix[:]...)
		hvcc = append(hvcc, nalu...)
	}

	if len(hvcc) == 0 {
		return h265AccessUnit{}, errors.New("h265 access unit only contained empty NAL units")
	}
	unit.HVCC = hvcc
	return unit, nil
}

func IsH265KeyframePayload(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// 1. Try Annex-B (start codes)
	nalus := splitH265NALUs(data)
	if len(nalus) > 0 {
		for _, nalu := range nalus {
			if len(nalu) == 0 {
				continue
			}
			naluType := (nalu[0] >> 1) & 0x3F
			// Only VCL keyframes (IDR, CRA, BLA) should satisfy the keyframe check
			if naluType >= 16 && naluType <= 21 {
				return true
			}
		}
	}

	// 2. Try length-prefixed (HVCC/AVCC style)
	ptr := 0
	for ptr+4 <= len(data) {
		naluSize := int(uint32(data[ptr])<<24 | uint32(data[ptr+1])<<16 | uint32(data[ptr+2])<<8 | uint32(data[ptr+3]))
		ptr += 4
		if ptr+naluSize > len(data) {
			break
		}
		if naluSize > 0 {
			naluType := (data[ptr] >> 1) & 0x3F
			if naluType >= 16 && naluType <= 21 {
				return true
			}
		}
		ptr += naluSize
	}

	// 3. Raw NAL unit fallback
	if len(data) > 0 {
		// Note: if Annex-B, this will check the first byte of the start code (0), which is fine as it's not 16-21.
		naluType := (data[0] >> 1) & 0x3F
		if naluType >= 16 && naluType <= 21 {
			return true
		}
	}

	return false
}

func splitH265NALUs(data []byte) [][]byte {
	var nalus [][]byte
	start := -1
	for i := 0; i < len(data)-2; i++ {
		if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if start != -1 && i > start {
				nalus = append(nalus, data[start:i])
			}
			start = i + 4
			i += 3 // Skip the rest of the start code
		} else if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start != -1 && i > start {
				nalus = append(nalus, data[start:i])
			}
			start = i + 3
			i += 2 // Skip the rest of the start code
		}
	}
	if start != -1 && start < len(data) {
		nalus = append(nalus, data[start:])
	}
	return nalus
}
