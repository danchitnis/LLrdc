package server

import "os"

type acceleratorMode string

const (
	acceleratorCPU      acceleratorMode = "cpu"
	acceleratorNVIDIA   acceleratorMode = "nvidia"
	acceleratorIntel    acceleratorMode = "intel"
	defaultIntelRender                  = "/dev/dri/renderD129"
	fallbackIntelRender                 = "/dev/dri/renderD128"
)

func currentAcceleratorMode() acceleratorMode {
	if UseIntel {
		return acceleratorIntel
	}
	if UseNVIDIA {
		return acceleratorNVIDIA
	}
	return acceleratorCPU
}

func usingHardwareAcceleration() bool {
	return currentAcceleratorMode() != acceleratorCPU
}

func resolveIntelRenderNode() string {
	if CaptureMode == CaptureModeDirect && currentAcceleratorMode() == acceleratorIntel {
		state := snapshotDirectBufferState()
		if state.RenderNode != "" {
			return state.RenderNode
		}
	}
	if _, err := os.Stat(defaultIntelRender); err == nil {
		return defaultIntelRender
	}
	return fallbackIntelRender
}

func splitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	start := 0
	for {
		// Find first start code
		sIdx, prefixLen, ok := findAnnexBStartCode(data, start)
		if !ok {
			break
		}

		// Find next start code to determine the end of this NALU
		nextStart := sIdx + prefixLen
		eIdx, _, ok := findAnnexBStartCode(data, nextStart)

		var nalu []byte
		if ok {
			nalu = data[sIdx+prefixLen : eIdx]
			start = eIdx
		} else {
			nalu = data[sIdx+prefixLen:]
			start = len(data)
		}

		// Trim trailing zeros from the NALU (they belong to the next start code's prefix)
		for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
			nalu = nalu[:len(nalu)-1]
		}

		if len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}

		if start >= len(data) {
			break
		}
	}
	return nalus
}
