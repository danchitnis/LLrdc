package server

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	CaptureModeCompat = "compat"
	CaptureModeDirect = "direct"
)

type directBufferStatus struct {
	Requested            bool   `json:"requested"`
	Supported            bool   `json:"supported"`
	Active               bool   `json:"active"`
	Reason               string `json:"reason"`
	CaptureMode          string `json:"captureMode"`
	RenderNode           string `json:"renderNode"`
	Renderer             string `json:"renderer"`
	ScreencopyAvailable  bool   `json:"screencopyAvailable"`
	LinuxDMABUFAvailable bool   `json:"linuxDmabufAvailable"`
}

type directBufferProbeResult struct {
	ScreencopyAvailable  bool
	LinuxDMABUFAvailable bool
}

var (
	directBufferMu     sync.RWMutex
	directBufferState  = directBufferStatus{CaptureMode: CaptureModeCompat, Reason: "Direct buffer disabled in compat mode"}
	errDirectModeCodec = errors.New("direct capture mode requires a hardware codec (NVENC or QSV/VAAPI)")
)

func isHardwareCodec(codec string) bool {
	return isNVENCCodec(codec) || isQSVCodec(codec)
}

func initDirectBufferState() {
	directBufferMu.Lock()
	defer directBufferMu.Unlock()

	directBufferState = directBufferStatus{
		Requested:   CaptureMode == CaptureModeDirect,
		Supported:   false,
		Active:      false,
		Reason:      "Direct buffer disabled in compat mode",
		CaptureMode: CaptureMode,
	}
	if CaptureMode == CaptureModeDirect {
		directBufferState.Reason = "Waiting for direct-buffer probe"
	}
}

func snapshotDirectBufferState() directBufferStatus {
	directBufferMu.RLock()
	defer directBufferMu.RUnlock()
	return directBufferState
}

func updateDirectBufferState(update func(*directBufferStatus)) {
	directBufferMu.Lock()
	defer directBufferMu.Unlock()
	update(&directBufferState)
}

func markDirectBufferProbeResult(renderNode string, supported bool, reason string, probe directBufferProbeResult) {
	updateDirectBufferState(func(state *directBufferStatus) {
		state.CaptureMode = CaptureMode
		state.Requested = CaptureMode == CaptureModeDirect
		state.RenderNode = renderNode
		state.Renderer = compatibleRendererName()
		state.Supported = supported
		state.Active = false
		state.Reason = reason
		state.ScreencopyAvailable = probe.ScreencopyAvailable
		state.LinuxDMABUFAvailable = probe.LinuxDMABUFAvailable
	})
}

func setDirectBufferActive(active bool, reason string) {
	updateDirectBufferState(func(state *directBufferStatus) {
		if state.CaptureMode != CaptureModeDirect {
			state.Active = false
			if reason != "" {
				state.Reason = reason
			}
			return
		}

		if state.Active != active && reason != "" {
			log.Printf("Direct-buffer active status changed to %v: %s", active, reason)
		}

		state.Active = active && state.Supported
		if reason != "" {
			state.Reason = reason
		}
	})
}

func compatibleRendererName() string {
	renderer := strings.TrimSpace(os.Getenv("WLR_RENDERER"))
	if renderer == "" {
		return "auto"
	}
	return renderer
}

func isNVENCCodec(codec string) bool {
	return codec == "h264_nvenc" || codec == "h265_nvenc" || codec == "av1_nvenc"
}

func validateCaptureModeConfig() error {
	if CaptureMode != CaptureModeCompat && CaptureMode != CaptureModeDirect {
		return fmt.Errorf("invalid capture mode %q", CaptureMode)
	}
	if CaptureMode != CaptureModeDirect {
		return nil
	}
	if !usingHardwareAcceleration() {
		return errors.New("direct capture mode requires --use-nvidia or --use-intel")
	}
	if !isHardwareCodec(VideoCodec) {
		return fmt.Errorf("%w: got %s", errDirectModeCodec, VideoCodec)
	}
	if UseIntel && VideoCodec == "h265_qsv" && !H265QSVAvailable {
		return errors.New("direct capture mode does not support Intel H.265 on the current FFmpeg/driver stack")
	}
	if Chroma != "420" {
		return fmt.Errorf("direct capture mode currently requires chroma 420, got %s", Chroma)
	}
	return nil
}

func validateRuntimeDirectMode(codec string, chroma string) error {
	if CaptureMode != CaptureModeDirect {
		return nil
	}
	if !isHardwareCodec(codec) {
		return fmt.Errorf("%w: got %s", errDirectModeCodec, codec)
	}
	if UseIntel && codec == "h265_qsv" && !H265QSVAvailable {
		return errors.New("direct capture mode does not support Intel H.265 on the current FFmpeg/driver stack")
	}
	if chroma != "420" {
		return fmt.Errorf("direct capture mode currently requires chroma 420, got %s", chroma)
	}
	state := snapshotDirectBufferState()
	if !state.Supported {
		return fmt.Errorf("direct capture mode requested but probe did not pass: %s", state.Reason)
	}
	return nil
}

func detectRenderNode() (string, error) {
	if currentAcceleratorMode() == acceleratorIntel {
		return resolveIntelRenderNode(), nil
	}

	nodes, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil {
		return "", err
	}
	// Sort nodes to prefer higher indices which are usually discrete GPUs
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if info, statErr := os.Stat(node); statErr == nil && !info.IsDir() {
			return node, nil
		}
	}
	return "", errors.New("no /dev/dri/renderD* device found")
}

func runDirectBufferProbe(env []string) (directBufferProbeResult, error) {
	cmd := exec.Command("direct_buffer_probe")
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()

	result := directBufferProbeResult{}
	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		switch line {
		case "screencopy=1":
			result.ScreencopyAvailable = true
		case "dmabuf=1":
			result.LinuxDMABUFAvailable = true
		}
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return result, nil
		}
		return result, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	return result, nil
}
