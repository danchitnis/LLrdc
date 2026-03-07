package main

import (
	"os"
	"strconv"
)

var (
	Port       int
	FPS        int
	DisplayNum string
	Display    string
	VideoCodec string
	UseGPU     bool
	UseDebugX11    bool
	UseDebugFFmpeg bool
)

func initConfig() {
	Port = 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil {
		Port = p
	}

	FPS = 30
	if f, err := strconv.Atoi(os.Getenv("FPS")); err == nil {
		FPS = f
	}

	VideoCodec = os.Getenv("VIDEO_CODEC")
	if VideoCodec == "" {
		VideoCodec = "vp8"
	}

	UseGPU = os.Getenv("USE_GPU") == "true"
	UseDebugX11 = os.Getenv("USE_DEBUG_X11") == "true"
	UseDebugFFmpeg = os.Getenv("USE_DEBUG_FFMPEG") == "true"

	DisplayNum = os.Getenv("DISPLAY_NUM")
	if DisplayNum == "" {
		DisplayNum = "99"
	}
	Display = ":" + DisplayNum
}
