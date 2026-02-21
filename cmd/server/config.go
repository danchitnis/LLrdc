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
	RtpPort    int
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

	DisplayNum = os.Getenv("DISPLAY_NUM")
	if DisplayNum == "" {
		DisplayNum = "99"
	}
	Display = ":" + DisplayNum

	RtpPort = Port + 4000
	if rp, err := strconv.Atoi(os.Getenv("RTP_PORT")); err == nil {
		RtpPort = rp
	}
}
