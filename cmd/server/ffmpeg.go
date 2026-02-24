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
	targetMode          = "bandwidth" // "bandwidth" or "quality"
	targetBandwidthMbps = 5 // Initial default: 5 Mbps
	targetQuality       = 70 // 10-100
	ffmpegCmd           *exec.Cmd
	ffmpegMutex         sync.Mutex
	ffmpegShouldRun     = true
)

func SetBandwidth(bwMbps int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetMode = "bandwidth"
	targetBandwidthMbps = bwMbps

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target bandwidth changed to %d Mbps, restarting ffmpeg...", bwMbps)
		ffmpegCmd.Process.Kill()
	}
}

func SetQuality(quality int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetMode = "quality"
	targetQuality = quality

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target quality changed to %d, restarting ffmpeg...", quality)
		ffmpegCmd.Process.Kill()
	}
}

func SetFramerate(fps int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	FPS = fps

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target framerate changed to %d fps, restarting ffmpeg...", fps)
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
			mode := targetMode
			bw := targetBandwidthMbps
			quality := targetQuality
			fps := FPS
			ffmpegMutex.Unlock()

			inputArgs := []string{"-framerate", fmt.Sprintf("%d", fps), "-f", "x11grab", "-video_size", "1280x720", "-i", Display}
			if os.Getenv("TEST_PATTERN") != "" {
				inputArgs = []string{"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=1280x720:rate=%d", fps)}
			}

			outputArgs := []string{
				"-pix_fmt", "yuv420p",
				"-c:v", "libvpx",
			}

			if mode == "bandwidth" {
				// Format bitrate dynamically,e.g 5 Mbps = "5000k"
				bitrateStr := fmt.Sprintf("%dk", bw*1000)
				// keep bufsize very small for low latency (e.g., 0.2s buffer)
				bufSizeStr := fmt.Sprintf("%dk", bw*200)

				outputArgs = append(outputArgs,
					"-b:v", bitrateStr,
					"-minrate", bitrateStr,
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
					"-crf", "10",
				)
			} else {
				// Quality mode: Map 10-100 to crf 50-4
				crf := 50 - (quality-10)*46/90
				if crf < 4 {
					crf = 4
				}
				if crf > 63 {
					crf = 63
				}
				// Scale maxrate with quality to give high quality more headroom
				// Quality 10 -> 2 Mbps, Quality 100 -> 20 Mbps
				maxKbps := 2000 + (quality-10)*18000/90
				maxrateStr := fmt.Sprintf("%dk", maxKbps)
				// Small buffer for low latency
				bufsizeStr := fmt.Sprintf("%dk", maxKbps/5)

				outputArgs = append(outputArgs,
					"-b:v", maxrateStr,
					"-maxrate", maxrateStr,
					"-bufsize", bufsizeStr,
					"-crf", fmt.Sprintf("%d", crf),
					"-qmin", fmt.Sprintf("%d", crf),
				)
			}

			outputArgs = append(outputArgs,
				"-rc_lookahead", "0",
				"-g", fmt.Sprintf("%d", fps),
				"-deadline", "realtime",
				"-cpu-used", "6",
				"-threads", "4",
				"-speed", "8",
				"-flush_packets", "1",
				"-f", "ivf",
				"pipe:1",
			)

			log.Printf("Starting ffmpeg capture (VP8) from %s at %s target...", Display, mode)

			args := append([]string{
				"-probesize", "32",
				"-analyzeduration", "0",
				"-fflags", "nobuffer",
				"-threads", "2",
			}, inputArgs...)
			args = append(args, outputArgs...)

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
