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
}
