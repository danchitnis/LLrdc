package client

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

func (a *NativeApp) CaptureSnapshotPNG() ([]byte, error) {
	if a.renderer == nil {
		return nil, errors.New("native renderer is unavailable")
	}
	return a.renderer.CaptureSnapshotPNG()
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

func (a *NativeApp) targetStreamSize(width, height int) (int, int) {
	a.mu.RLock()
	maxRes := a.currentResolution().Value
	a.mu.RUnlock()
	return targetStreamSizeForResolution(width, height, maxRes)
}

func targetStreamSizeForResolution(width, height, maxRes int) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	if maxRes > 0 {
		ratio := float64(width) / float64(height)
		height = maxRes
		width = int(math.Round(float64(height) * ratio))
		if ratio > 1.2 {
			switch maxRes {
			case 720:
				width = 1280
			case 1080:
				width = 1920
			case 1440:
				width = 2560
			case 2160:
				width = 3840
			}
		}
		return width, height
	}
	if height > 2160 {
		ratio := 2160.0 / float64(height)
		height = 2160
		width = int(math.Round(float64(width) * ratio))
	}
	return width, height
}

func floatFromAny(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func floatOrZero(v any) float64 {
	value, _ := floatFromAny(v)
	return value
}

func stringFromAny(v any) string {
	if value, ok := v.(string); ok {
		return value
	}
	return ""
}

func intFromString(v string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(v))
	return value
}
