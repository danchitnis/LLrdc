package main

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
