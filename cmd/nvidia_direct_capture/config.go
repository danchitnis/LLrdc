package main

import (
	"flag"
	"fmt"
)

type CaptureConfig struct {
	Width          int
	Height         int
	FPS            int
	Codec          string
	Bitrate        int
	Chroma         string
	RenderNode     string
	DamageTracking bool
	VBR            bool
	VBRThreshold   int
}

func parseConfig() (*CaptureConfig, error) {
	config := &CaptureConfig{}

	flag.IntVar(&config.Width, "width", 1920, "Capture width")
	flag.IntVar(&config.Height, "height", 1080, "Capture height")
	flag.IntVar(&config.FPS, "fps", 30, "Capture frame rate")
	flag.StringVar(&config.Codec, "codec", "h264", "Target codec (h264, hevc, av1)")
	flag.IntVar(&config.Bitrate, "bitrate", 10, "Target bitrate in Mbps")
	flag.StringVar(&config.Chroma, "chroma", "420", "Chroma format (420, 444)")
	flag.StringVar(&config.RenderNode, "render-node", "/dev/dri/renderD129", "DRM/NVIDIA render node path")
	flag.BoolVar(&config.DamageTracking, "damage-tracking", false, "Enable compositor damage tracking")
	flag.BoolVar(&config.VBR, "vbr", false, "Enable Variable Bitrate (VBR)")
	flag.IntVar(&config.VBRThreshold, "vbr-threshold", 0, "VBR threshold value")

	flag.Parse()

	if config.Codec != "h264" && config.Codec != "hevc" && config.Codec != "h265" && config.Codec != "av1" &&
		config.Codec != "h264_nvenc" && config.Codec != "hevc_nvenc" && config.Codec != "h265_nvenc" && config.Codec != "av1_nvenc" {
		return nil, fmt.Errorf("unsupported codec %s", config.Codec)
	}

	if config.Chroma != "420" && config.Chroma != "444" {
		return nil, fmt.Errorf("unsupported chroma format %s", config.Chroma)
	}

	return config, nil
}
