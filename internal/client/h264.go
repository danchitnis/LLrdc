package client

import (
	"encoding/binary"
	"errors"
)

type h264AccessUnit struct {
	AVCC []byte
	SPS  []byte
	PPS  []byte
}

func buildH264AccessUnit(frame []byte) (h264AccessUnit, error) {
	nalus := splitH264NALUs(frame)
	if len(nalus) == 0 {
		return h264AccessUnit{}, errors.New("h264 access unit did not contain any NAL units")
	}

	avcc := make([]byte, 0, len(frame)+len(nalus)*4)
	unit := h264AccessUnit{}
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		switch nalu[0] & 0x1F {
		case 7:
			unit.SPS = append(unit.SPS[:0], nalu...)
		case 8:
			unit.PPS = append(unit.PPS[:0], nalu...)
		}

		var sizePrefix [4]byte
		binary.BigEndian.PutUint32(sizePrefix[:], uint32(len(nalu)))
		avcc = append(avcc, sizePrefix[:]...)
		avcc = append(avcc, nalu...)
	}

	if len(avcc) == 0 {
		return h264AccessUnit{}, errors.New("h264 access unit only contained empty NAL units")
	}
	unit.AVCC = avcc
	return unit, nil
}

func splitH264NALUs(frame []byte) [][]byte {
	if len(frame) == 0 {
		return nil
	}

	var nalus [][]byte
	start := -1
	i := 0
	for i < len(frame) {
		scLen := h264StartCodeLength(frame[i:])
		if scLen == 0 {
			i++
			continue
		}

		if start >= 0 && start < i {
			nalu := trimH264Delimiter(frame[start:i])
			if len(nalu) > 0 {
				nalus = append(nalus, nalu)
			}
		}
		i += scLen
		start = i
	}

	if start >= 0 && start <= len(frame) {
		nalu := trimH264Delimiter(frame[start:])
		if len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}
	}

	if len(nalus) > 0 {
		return nalus
	}
	return [][]byte{trimH264Delimiter(frame)}
}

func h264StartCodeLength(data []byte) int {
	if len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return 4
	}
	if len(data) >= 3 && data[0] == 0 && data[1] == 0 && data[2] == 1 {
		return 3
	}
	return 0
}

func trimH264Delimiter(data []byte) []byte {
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}
	if len(data) > 0 && data[0] == 1 {
		data = data[1:]
	}
	return data
}
