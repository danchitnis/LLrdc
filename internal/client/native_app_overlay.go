package client

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func (a *NativeApp) overlayState() any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.overlayStateLocked()
}

func (a *NativeApp) overlayStateLocked() OverlayState {
	overlay := OverlayState{
		HUDColor: a.lastHUDColor,
	}
	if a.showStats && a.lastHUDText != "" {
		overlay.HUDLines = []string{a.lastHUDText}
	}
	if a.menuVisible {
		items := a.visibleMenuItemsLocked()
		overlay.MenuVisible = true
		overlay.MenuTitle = "LLRDC NATIVE MENU"
		overlay.MenuHint = a.menuHintLocked()
		overlay.SelectedIndex = a.menuSelected
		overlay.MenuItems = make([]string, 0, len(items))
		for idx, item := range items {
			prefix := "  "
			if idx == a.menuSelected {
				prefix = "> "
			}
			line := prefix
			if item.Depth > 0 {
				line += strings.Repeat("  ", item.Depth)
			}
			if item.Expandable {
				if item.Expanded {
					line += "[-] "
				} else {
					line += "[+] "
				}
			} else if item.Depth > 0 {
				line += "- "
			}
			line += strings.ToUpper(item.Label)
			if item.Value != "" {
				line += " [" + strings.ToUpper(item.Value) + "]"
			}
			if item.Current {
				line += " [CURRENT]"
			}
			if !item.Enabled {
				line += " (DISABLED)"
			}
			overlay.MenuItems = append(overlay.MenuItems, line)
		}
	}
	return overlay
}

func (a *NativeApp) refreshOverlay() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshOverlayLocked()
}

func (a *NativeApp) refreshOverlayLocked() {
	if a.renderer == nil {
		return
	}

	now := time.Now()
	if !a.showStats {
		a.lastHUDText = ""
		a.lastHUDColor = OverlayColor{R: 68, G: 255, B: 68, A: 255}
		a.lastHUDUpdateAt = time.Time{}
	} else if a.lastHUDUpdateAt.IsZero() || now.Sub(a.lastHUDUpdateAt) >= statsHUDRefreshInterval {
		a.lastHUDText, a.lastHUDColor = a.buildHUDLocked(now)
		a.lastHUDUpdateAt = now
	}
	a.renderer.SetOverlayState(a.overlayStateLocked())
}

func (a *NativeApp) buildHUDLocked(now time.Time) (string, OverlayColor) {
	if !a.showStats {
		return "", OverlayColor{R: 68, G: 255, B: 68, A: 255}
	}

	state := a.session.State()

	fps := rollingPresentedFPS(now, state.RecentLatencySamples)
	bwMbps := rollingVideoMbps(now, state.RecentVideoByteSamples)
	displayCodec := strings.TrimPrefix(strings.ToUpper(state.VideoCodec), "VIDEO/")
	if displayCodec == "" {
		displayCodec = "AUTO"
	}
	res := ""
	if state.LastPresentedWidth > 0 && state.LastPresentedHeight > 0 {
		res = fmt.Sprintf("%dx%d ", state.LastPresentedWidth, state.LastPresentedHeight)
	}

	avgLat := rollingLatencyMs(now, state.RecentLatencySamples)

	color := OverlayColor{R: 68, G: 255, B: 68, A: 255}
	if avgLat > 150 {
		color = OverlayColor{R: 255, G: 170, B: 68, A: 255}
	}
	if avgLat > 300 {
		color = OverlayColor{R: 255, G: 68, B: 68, A: 255}
	}

	text := fmt.Sprintf("[%s] %s%d FPS | LAT %dMS | BW %.1fMb", displayCodec, res, fps, int(avgLat), bwMbps)
	if ffmpegCPU, ok := state.LastStats["ffmpegCpu"].(float64); ok {
		text += fmt.Sprintf(" | CPU %d%%", int(ffmpegCPU))
	}
	if gpuUtil, ok := state.LastStats["intelGpuUtil"].(float64); ok && gpuUtil > 0 {
		text += fmt.Sprintf(" | ENC %d%%", int(gpuUtil))
	}
	return text, color
}

func rollingPresentedFPS(now time.Time, samples []LatencyBreakdown) uint64 {
	window := latencySamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}
	fps := float64(len(window)) * 1000.0 / float64(statsMetricWindowMs)
	if fps < 0 {
		return 0
	}
	return uint64(math.Round(fps))
}

func rollingVideoMbps(now time.Time, samples []TimedByteSample) float64 {
	window := byteSamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}

	totalBytes := 0
	for _, sample := range window {
		totalBytes += sample.Bytes
	}
	seconds := float64(statsMetricWindowMs) / 1000.0
	return float64(totalBytes) * 8 / 1024 / 1024 / seconds
}

func rollingLatencyMs(now time.Time, samples []LatencyBreakdown) float64 {
	window := latencySamplesInWindow(now, samples)
	if len(window) == 0 {
		return 0
	}

	sum := 0.0
	count := 0
	for _, sample := range window {
		if sample.PresentationAt <= 0 || sample.ReceiveAt <= 0 || sample.PresentationAt < sample.ReceiveAt {
			continue
		}
		sum += float64(sample.PresentationAt - sample.ReceiveAt)
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func latencySampleReferenceMs(now time.Time, samples []LatencyBreakdown) int64 {
	nowMs := now.UnixMilli()
	if len(samples) == 0 {
		return nowMs
	}

	last := samples[len(samples)-1].PresentationAt
	if last <= 0 {
		return nowMs
	}

	const maxExpectedClockSkewMs = int64(60 * 60 * 1000)
	if delta := nowMs - last; delta > maxExpectedClockSkewMs || delta < -maxExpectedClockSkewMs {
		return last
	}
	return nowMs
}

func latencySamplesInWindow(now time.Time, samples []LatencyBreakdown) []LatencyBreakdown {
	if len(samples) == 0 {
		return nil
	}
	cutoff := latencySampleReferenceMs(now, samples) - statsMetricWindowMs
	start := 0
	for start < len(samples) && samples[start].PresentationAt <= cutoff {
		start++
	}
	return samples[start:]
}

func byteSamplesInWindow(now time.Time, samples []TimedByteSample) []TimedByteSample {
	if len(samples) == 0 {
		return nil
	}
	cutoff := now.UnixMilli() - statsMetricWindowMs
	start := 0
	for start < len(samples) && samples[start].AtMs <= cutoff {
		start++
	}
	return samples[start:]
}
