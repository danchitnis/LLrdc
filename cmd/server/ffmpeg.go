package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	targetBandwidthMbps = 5 // Initial default: 5 Mbps
	ffmpegCmd           *exec.Cmd
	ffmpegMutex         sync.Mutex
	ffmpegShouldRun     = true
)

func SetBandwidth(bwMbps int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetBandwidthMbps = bwMbps

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target bandwidth changed to %d Mbps, restarting ffmpeg...", bwMbps)
		ffmpegCmd.Process.Kill()
	}
}

func startStreaming(onFrame func([]byte)) {
	ffmpegPath := "/app/bin/ffmpeg"
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		log.Println("Warning: /app/bin/ffmpeg not found, relying on system PATH")
		ffmpegPath = "ffmpeg"
	}
	
	cleanupTasks = append(cleanupTasks, func() {
		ffmpegMutex.Lock()
		defer ffmpegMutex.Unlock()
		ffmpegShouldRun = false
		if ffmpegCmd != nil && ffmpegCmd.Process != nil {
			log.Println("Killing ffmpeg (cleanup)...")
			ffmpegCmd.Process.Kill()
		}
	})

	go func() {
		for {
			ffmpegMutex.Lock()
			if !ffmpegShouldRun {
				ffmpegMutex.Unlock()
				break
			}
			bw := targetBandwidthMbps
			ffmpegMutex.Unlock()

			inputArgs := []string{"-f", "x11grab", "-video_size", "1280x720", "-i", Display}
			if os.Getenv("TEST_PATTERN") != "" {
				inputArgs = []string{"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=1280x720:rate=%d", FPS)}
			}

			// Format bitrate dynamically,e.g 5 Mbps = "5000k"
			bitrateStr := fmt.Sprintf("%dk", bw*1000)
			// keep bufsize somewhat smaller for low latency, maybe half of bitrate
			bufSizeStr := fmt.Sprintf("%dk", bw*500)

			outputArgs := []string{
				"-vf", fmt.Sprintf("fps=%d,format=yuv420p", FPS),
				"-c:v", "libvpx",
				"-b:v", bitrateStr,
				"-minrate", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
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

			log.Printf("Starting ffmpeg capture (VP8) from %s at %d Mbps...", Display, bw)

			cmd := exec.Command(ffmpegPath, args...)
			cmd.Env = append(os.Environ(), "DISPLAY="+Display)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Fatalf("Failed to get stdout from ffmpeg: %v", err)
			}
			
			ffmpegMutex.Lock()
			ffmpegCmd = cmd
			ffmpegMutex.Unlock()

			if err := cmd.Start(); err != nil {
				log.Fatalf("Failed to start ffmpeg: %v", err)
			}

			// Start IVF splitting in a bounded way
			doneCh := make(chan struct{})
			go func() {
				splitIVF(stdout, onFrame)
				close(doneCh)
			}()

			// Wait for IVF splitter to finish reading pipeline to avoid Wait closing stdout prematurely
			<-doneCh
			
			err = cmd.Wait()
			log.Printf("ffmpeg exited: %v", err)
			

			ffmpegMutex.Lock()
			shouldRun := ffmpegShouldRun
			ffmpegMutex.Unlock()
			
			if !shouldRun {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()
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
