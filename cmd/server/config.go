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
	Port                    int
	FPS                     int
	DisplayNum              string
	Display                 string
	VideoCodec              string
	Chroma                  string
	UseGPU                  bool

	AV1NVENCAvailable       bool
	H264NVENC444Available   bool
	UseDebugX11             bool
	UseDebugFFmpeg          bool
	TestPattern             bool
	TestMinimalX11          bool
	EnableClipboard         bool
	Wallpaper               string
	WebRTCPublicIP          string
	WebRTCInterfaces        string
	WebRTCExcludeInterfaces string
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

	defaultVideoCodec := os.Getenv("VIDEO_CODEC")
	if defaultVideoCodec == "" {
		defaultVideoCodec = "vp8"
	}

	defaultChroma := os.Getenv("CHROMA")
	if defaultChroma == "" {
		defaultChroma = "420"
	}

	defaultUseGPU := os.Getenv("USE_GPU") == "true"
	defaultUseDebugX11 := os.Getenv("USE_DEBUG_X11") == "true"
	defaultUseDebugFFmpeg := os.Getenv("USE_DEBUG_FFMPEG") == "true"
	defaultTestPattern := os.Getenv("TEST_PATTERN") != ""
	defaultTestMinimalX11 := os.Getenv("TEST_MINIMAL_X11") != ""
	defaultEnableClipboard := os.Getenv("ENABLE_CLIPBOARD") != "false"

	defaultDisplayNum := os.Getenv("DISPLAY_NUM")
	if defaultDisplayNum == "" {
		defaultDisplayNum = "99"
	}

	defaultWallpaper := os.Getenv("WALLPAPER")
	defaultWebRTCPublicIP := os.Getenv("WEBRTC_PUBLIC_IP")
	defaultWebRTCInterfaces := os.Getenv("WEBRTC_INTERFACES")
	defaultWebRTCExcludeInterfaces := os.Getenv("WEBRTC_EXCLUDE_INTERFACES")

	// Custom Usage format
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of llrdc:\n")
		fmt.Fprintf(os.Stderr, "  llrdc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: --port configures both the HTTP and WebRTC UDP port.\n\n")

		fmt.Fprintf(os.Stderr, "User Flags:\n")
		printFlag(os.Stderr, "port", "Port for HTTP and WebRTC UDP", Port)
		printFlag(os.Stderr, "fps", "Target framerate", FPS)
		printFlag(os.Stderr, "video-codec", "Video codec (vp8, h264, h264_nvenc, av1, av1_nvenc)", VideoCodec)
		printFlag(os.Stderr, "chroma", "Chroma subsampling format (420 or 444)", Chroma)
		printFlag(os.Stderr, "use-gpu", "Enable GPU acceleration if available", UseGPU)
		printFlag(os.Stderr, "use-debug-x11", "Enable X11 debugging", UseDebugX11)
		printFlag(os.Stderr, "use-debug-ffmpeg", "Enable FFmpeg debugging", UseDebugFFmpeg)
		printFlag(os.Stderr, "display-num", "X11 Display number (e.g., 99 for :99)", DisplayNum)
		printFlag(os.Stderr, "wallpaper", "Path to wallpaper image", Wallpaper)
		printFlag(os.Stderr, "webrtc-public-ip", "Public IP for WebRTC", WebRTCPublicIP)
		printFlag(os.Stderr, "webrtc-interfaces", "Comma-separated allowed network interfaces for WebRTC", WebRTCInterfaces)
		printFlag(os.Stderr, "webrtc-exclude-interfaces", "Comma-separated excluded network interfaces for WebRTC", WebRTCExcludeInterfaces)
		printFlag(os.Stderr, "enable-clipboard", "Enable clipboard synchronization", EnableClipboard)

		fmt.Fprintf(os.Stderr, "\nTesting Flags:\n")
		printFlag(os.Stderr, "test-pattern", "Run with test pattern instead of X11", TestPattern)
		printFlag(os.Stderr, "test-minimal-x11", "Start minimal X11 without full DE", TestMinimalX11)
	}

	// Define flags
	flag.IntVar(&Port, "port", defaultPort, "Port for HTTP and WebRTC UDP")
	flag.IntVar(&FPS, "fps", defaultFPS, "Target framerate")
	flag.StringVar(&VideoCodec, "video-codec", defaultVideoCodec, "Video codec (vp8, h264, h264_nvenc, av1, av1_nvenc)")
	flag.StringVar(&Chroma, "chroma", defaultChroma, "Chroma subsampling format (420 or 444)")
	flag.BoolVar(&UseGPU, "use-gpu", defaultUseGPU, "Enable GPU acceleration if available")
	flag.BoolVar(&UseDebugX11, "use-debug-x11", defaultUseDebugX11, "Enable X11 debugging")
	flag.BoolVar(&UseDebugFFmpeg, "use-debug-ffmpeg", defaultUseDebugFFmpeg, "Enable FFmpeg debugging")
	flag.StringVar(&DisplayNum, "display-num", defaultDisplayNum, "X11 Display number (e.g., 99 for :99)")
	flag.BoolVar(&TestPattern, "test-pattern", defaultTestPattern, "Run with test pattern instead of X11")
	flag.BoolVar(&TestMinimalX11, "test-minimal-x11", defaultTestMinimalX11, "Start minimal X11 without full DE")
	flag.StringVar(&Wallpaper, "wallpaper", defaultWallpaper, "Path to wallpaper image")
	flag.StringVar(&WebRTCPublicIP, "webrtc-public-ip", defaultWebRTCPublicIP, "Public IP for WebRTC")
	flag.StringVar(&WebRTCInterfaces, "webrtc-interfaces", defaultWebRTCInterfaces, "Comma-separated allowed network interfaces for WebRTC")
	flag.StringVar(&WebRTCExcludeInterfaces, "webrtc-exclude-interfaces", defaultWebRTCExcludeInterfaces, "Comma-separated excluded network interfaces for WebRTC")
	flag.BoolVar(&EnableClipboard, "enable-clipboard", defaultEnableClipboard, "Enable clipboard synchronization")

	flag.Parse()

	Display = ":" + DisplayNum

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
	}
}

func printFlag(w *os.File, name, usage string, def any) {
	fmt.Fprintf(w, "  -%s\n    \t%s (default %v)\n", name, usage, def)
}
