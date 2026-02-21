package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

func startStreaming(onFrame func([]byte)) {
	ffmpegPath := "/app/bin/ffmpeg"
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		log.Println("Warning: /app/bin/ffmpeg not found, relying on system PATH")
		ffmpegPath = "ffmpeg"
	}

	inputArgs := []string{"-f", "x11grab", "-video_size", "1280x720", "-i", Display}
	if os.Getenv("TEST_PATTERN") != "" {
		inputArgs = []string{"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=1280x720:rate=%d", FPS)}
	}

	outputArgs := []string{
		"-vf", fmt.Sprintf("fps=%d,format=yuv420p", FPS),
		"-c:v", "libvpx",
		"-b:v", "2000k",
		"-minrate", "2000k",
		"-maxrate", "2000k",
		"-bufsize", "500k",
		"-rc_lookahead", "0",
		"-crf", "10",
		"-g", "30",
		"-deadline", "realtime",
		"-cpu-used", "6",
		"-threads", "4",
		"-speed", "8",
		"-map", "0:v",
		"-f", "tee",
		fmt.Sprintf("[f=rtp:payload_type=96]rtp://127.0.0.1:%d?pkt_size=1000|[f=ivf]pipe:1", RtpPort),
	}

	args := append([]string{
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-threads", "2",
	}, inputArgs...)
	args = append(args, outputArgs...)

	log.Printf("Starting ffmpeg capture (VP8) from %s...", Display)

	cmd := exec.Command(ffmpegPath, args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+Display)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to get stdout from ffmpeg: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start ffmpeg: %v", err)
	}

	cleanupTasks = append(cleanupTasks, func() {
		log.Println("Killing ffmpeg...")
		cmd.Process.Kill()
	})

	go func() {
		err := cmd.Wait()
		log.Printf("ffmpeg exited: %v", err)
		time.Sleep(1 * time.Second)
		log.Println("Restarting ffmpeg...")
		startStreaming(onFrame)
	}()

	go splitIVF(stdout, onFrame)
}

func splitIVF(reader io.Reader, onFrame func([]byte)) {
	headerData := make([]byte, 32)
	if _, err := io.ReadFull(reader, headerData); err != nil {
		log.Printf("Failed to read IVF header: %v", err)
		return
	}
	if string(headerData[:4]) != "DKIF" {
		log.Printf("Invalid IVF signature: %s", string(headerData[:4]))
		return
	}

	for {
		frameHeader := make([]byte, 12)
		if _, err := io.ReadFull(reader, frameHeader); err != nil {
			if err != io.EOF {
				log.Printf("Error reading frame header: %v", err)
			}
			return
		}

		frameSize := binary.LittleEndian.Uint32(frameHeader[0:4])
		frameData := make([]byte, frameSize)
		if _, err := io.ReadFull(reader, frameData); err != nil {
			log.Printf("Error reading frame data: %v", err)
			return
		}

		onFrame(frameData)
	}
}
