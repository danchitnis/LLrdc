package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	Port        int
	FPS         int
	VideoCodec  string
	Chroma      string
	UseGPU      bool
	CaptureMode string

	AV1NVENCAvailable       bool
	H264NVENC444Available   bool
	H265NVENC444Available   bool
	UseDebugFFmpeg          bool
	UseDebugInput           bool
	TestPattern             bool
	EnableAudio             bool
	AudioBitrate            string
	Wallpaper               string
	WebRTCPublicIP          string
	WebRTCInterfaces        string
	WebRTCExcludeInterfaces string
	HDPI                    int
	SettleTime              int
	TileSize                int
)

func initConfig() {
	// Fallback from environment variables
	defaultPort := 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil {
		defaultPort = p
	}

	defaultFPS := 30
	if f, err := strconv.Atoi(os.Getenv("FPS")); err == nil {
		defaultFPS = f
	}

	defaultBandwidth := targetBandwidthMbps
	if bw, err := strconv.Atoi(os.Getenv("BANDWIDTH")); err == nil {
		defaultBandwidth = bw
	}

	defaultVideoCodec := os.Getenv("VIDEO_CODEC")
	if defaultVideoCodec == "" {
		defaultVideoCodec = "vp8"
	}

	defaultChroma := os.Getenv("CHROMA")
	if defaultChroma == "" {
		defaultChroma = "420"
	}

	defaultUseGPU := os.Getenv("USE_GPU") == "true"
	defaultCaptureMode := os.Getenv("CAPTURE_MODE")
	if defaultCaptureMode == "" {
		defaultCaptureMode = CaptureModeCompat
	}
	defaultUseDebugFFmpeg := os.Getenv("USE_DEBUG_FFMPEG") == "true"
	defaultUseDebugInput := os.Getenv("USE_DEBUG_INPUT") == "true"
	defaultTestPattern := os.Getenv("TEST_PATTERN") != ""
	defaultEnableAudio := os.Getenv("ENABLE_AUDIO") != "false"
	defaultAudioBitrate := os.Getenv("AUDIO_BITRATE")
	if defaultAudioBitrate == "" {
		defaultAudioBitrate = "128k"
	}

	defaultWallpaper := os.Getenv("WALLPAPER")
	defaultWebRTCPublicIP := os.Getenv("WEBRTC_PUBLIC_IP")
	defaultWebRTCInterfaces := os.Getenv("WEBRTC_INTERFACES")
	defaultWebRTCExcludeInterfaces := os.Getenv("WEBRTC_EXCLUDE_INTERFACES")

	defaultHDPI := 0
	if hdpi, err := strconv.Atoi(os.Getenv("HDPI")); err == nil {
		defaultHDPI = hdpi
	}

	defaultSettleTime := 500
	if st, err := strconv.Atoi(os.Getenv("SETTLE_TIME")); err == nil {
		defaultSettleTime = st
	}

	defaultTileSize := 128
	if ts, err := strconv.Atoi(os.Getenv("TILE_SIZE")); err == nil {
		defaultTileSize = ts
	}

	// Custom Usage format
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of llrdc:\n")
		fmt.Fprintf(os.Stderr, "  llrdc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: --port configures both the HTTP and WebRTC UDP port.\n\n")

		fmt.Fprintf(os.Stderr, "User Flags:\n")
		printFlag(os.Stderr, "port", "Port for HTTP and WebRTC UDP", Port)
		printFlag(os.Stderr, "fps", "Target framerate", FPS)
		printFlag(os.Stderr, "bandwidth", "Target bandwidth in Mbps", targetBandwidthMbps)
		printFlag(os.Stderr, "video-codec", "Video codec (vp8, h264, h264_nvenc, h265, h265_nvenc, av1, av1_nvenc)", VideoCodec)
		printFlag(os.Stderr, "chroma", "Chroma subsampling format (420 or 444)", Chroma)
		printFlag(os.Stderr, "use-gpu", "Enable GPU acceleration if available", UseGPU)
		printFlag(os.Stderr, "capture-mode", "Capture mode (compat or direct)", CaptureMode)
		printFlag(os.Stderr, "use-debug-ffmpeg", "Enable FFmpeg debugging", UseDebugFFmpeg)
		printFlag(os.Stderr, "wallpaper", "Path to wallpaper image", Wallpaper)
		printFlag(os.Stderr, "webrtc-public-ip", "Public IP for WebRTC", WebRTCPublicIP)
		printFlag(os.Stderr, "webrtc-interfaces", "Comma-separated allowed network interfaces for WebRTC", WebRTCInterfaces)
		printFlag(os.Stderr, "webrtc-exclude-interfaces", "Comma-separated excluded network interfaces for WebRTC", WebRTCExcludeInterfaces)
		printFlag(os.Stderr, "enable-audio", "Enable audio streaming", EnableAudio)
		printFlag(os.Stderr, "audio-bitrate", "Audio bitrate (e.g. 64k, 128k)", AudioBitrate)
		printFlag(os.Stderr, "hdpi", "Set high DPI scaling percentage (e.g., 150, 200)", HDPI)

		fmt.Fprintf(os.Stderr, "\nTesting Flags:\n")
		printFlag(os.Stderr, "test-pattern", "Run with test pattern instead of Wayland session", TestPattern)
		printFlag(os.Stderr, "settle-time", "Hybrid sharpness settle time (ms)", SettleTime)
		printFlag(os.Stderr, "tile-size", "Hybrid sharpness tile size (px)", TileSize)
	}

	defaultVBR := true
	if vbr, err := strconv.ParseBool(os.Getenv("VBR")); err == nil {
		defaultVBR = vbr
	}

	// Define flags
	flag.IntVar(&Port, "port", defaultPort, "Port for HTTP and WebRTC UDP")
	flag.IntVar(&FPS, "fps", defaultFPS, "Target framerate")
	flag.IntVar(&targetBandwidthMbps, "bandwidth", defaultBandwidth, "Target bandwidth in Mbps")
	flag.StringVar(&VideoCodec, "video-codec", defaultVideoCodec, "Video codec (vp8, h264, h264_nvenc, h265, h265_nvenc, av1, av1_nvenc)")
	flag.StringVar(&Chroma, "chroma", defaultChroma, "Chroma subsampling format (420 or 444)")
	flag.BoolVar(&UseGPU, "use-gpu", defaultUseGPU, "Enable GPU acceleration if available")
	flag.StringVar(&CaptureMode, "capture-mode", defaultCaptureMode, "Capture mode (compat or direct)")
	flag.BoolVar(&UseDebugFFmpeg, "use-debug-ffmpeg", defaultUseDebugFFmpeg, "Enable FFmpeg debugging")
	flag.BoolVar(&UseDebugInput, "use-debug-input", defaultUseDebugInput, "Enable Input debugging")
	flag.BoolVar(&TestPattern, "test-pattern", defaultTestPattern, "Run with test pattern instead of Wayland session")
	flag.StringVar(&Wallpaper, "wallpaper", defaultWallpaper, "Path to wallpaper image")
	flag.StringVar(&WebRTCPublicIP, "webrtc-public-ip", defaultWebRTCPublicIP, "Public IP for WebRTC")
	flag.StringVar(&WebRTCInterfaces, "webrtc-interfaces", defaultWebRTCInterfaces, "Comma-separated allowed network interfaces for WebRTC")
	flag.StringVar(&WebRTCExcludeInterfaces, "webrtc-exclude-interfaces", defaultWebRTCExcludeInterfaces, "Comma-separated excluded network interfaces for WebRTC")
	EnableAudio = true
	flag.BoolVar(&EnableAudio, "enable-audio", defaultEnableAudio, "Enable audio streaming")
	flag.StringVar(&AudioBitrate, "audio-bitrate", defaultAudioBitrate, "Audio bitrate (e.g. 64k, 128k)")
	flag.IntVar(&HDPI, "hdpi", defaultHDPI, "Set high DPI scaling percentage (e.g., 150, 200)")
	flag.IntVar(&SettleTime, "settle-time", defaultSettleTime, "Hybrid sharpness settle time (ms)")
	flag.IntVar(&TileSize, "tile-size", defaultTileSize, "Hybrid sharpness tile size (px)")
	flag.BoolVar(&targetVBR, "vbr", defaultVBR, "Enable variable bitrate (damage tracking)")

	flag.Parse()
	initDirectBufferState()
	if err := validateCaptureModeConfig(); err != nil {
		log.Fatalf("Invalid direct-buffer configuration: %v", err)
	}

	if UseGPU {
		log.Printf("Checking NVIDIA GPU capabilities...")

		// Check basic AV1 support via encoders list
		outAV1, _ := exec.Command("bash", "-c", "ffmpeg -hide_banner -encoders | grep -q av1_nvenc && echo true || echo false").Output()
		AV1NVENCAvailable = strings.TrimSpace(string(outAV1)) == "true"

		if AV1NVENCAvailable {
			log.Printf("AV1 NVENC support detected")
			// Note: AV1 NVENC does NOT support 4:4:4 chroma on any current NVIDIA GPU.
		}

		log.Printf("Checking H.264 NVENC 4:4:4 support...")
		outH264, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v h264_nvenc -profile:v high444p -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H264NVENC444Available = strings.TrimSpace(string(outH264)) == "true"
		if H264NVENC444Available {
			log.Printf("H.264 NVENC 4:4:4 support detected")
		} else {
			log.Printf("H.264 NVENC 4:4:4 support NOT detected")
		}

		log.Printf("Checking H.265 NVENC 4:4:4 support...")
		outH265, _ := exec.Command("bash", "-c", "ffmpeg -y -f lavfi -i testsrc=size=256x256:rate=1 -t 1 -pix_fmt yuv444p -c:v hevc_nvenc -profile:v rext -f null - > /dev/null 2>&1 && echo true || echo false").Output()
		H265NVENC444Available = strings.TrimSpace(string(outH265)) == "true"
		if H265NVENC444Available {
			log.Printf("H.265 NVENC 4:4:4 support detected")
		} else {
			log.Printf("H.265 NVENC 4:4:4 support NOT detected")
		}
	}
}

func printFlag(w *os.File, name, usage string, def any) {
	fmt.Fprintf(w, "  -%s\n    \t%s (default %v)\n", name, usage, def)
}
