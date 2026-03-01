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
	targetBandwidthMbps = 5           // Initial default: 5 Mbps
	targetQuality       = 70          // 10-100
	targetVBR           = true        // Default VBR to true
	targetCpuEffort     = 6           // Default: 6
	targetCpuThreads    = 4           // Default: 4
	targetDrawMouse     = true        // Default: true
	ffmpegCmd           *exec.Cmd
	ffmpegMutex         sync.Mutex
	ffmpegShouldRun     = true
	ffmpegStreamID      uint32
)

func SetCpuEffort(effort int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetCpuEffort = effort

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target CPU effort changed to %d, restarting ffmpeg...", effort)
		ffmpegCmd.Process.Kill()
	}
}

func SetCpuThreads(threads int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetCpuThreads = threads

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target CPU threads changed to %d, restarting ffmpeg...", threads)
		ffmpegCmd.Process.Kill()
	}
}

func SetDrawMouse(draw bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetDrawMouse = draw

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target draw mouse changed to %v, restarting ffmpeg...", draw)
		ffmpegCmd.Process.Kill()
	}
}

func SetVBR(vbr bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetVBR = vbr

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target VBR changed to %v, restarting ffmpeg...", vbr)
		ffmpegCmd.Process.Kill()
	}
}

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

func RestartForResize() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Println("Screen size changed, restarting ffmpeg...")
		ffmpegCmd.Process.Kill()
	}
}

func startStreaming(onFrame func([]byte, uint32)) {
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
			vbr := targetVBR
			cpuEffort := targetCpuEffort
			cpuThreads := targetCpuThreads
			drawMouse := targetDrawMouse
			ffmpegMutex.Unlock()

			width, height := GetScreenSize()
			size := fmt.Sprintf("%dx%d", width, height)
			
			drawMouseStr := "0"
			if drawMouse {
				drawMouseStr = "1"
			}
			
			inputArgs := []string{"-framerate", fmt.Sprintf("%d", fps), "-f", "x11grab", "-draw_mouse", drawMouseStr, "-video_size", size, "-i", Display + ".0"}
			if os.Getenv("TEST_PATTERN") != "" {
				inputArgs = []string{"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%s:rate=%d", size, fps)}
			}

			outputArgs := []string{
				"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
				"-pix_fmt", "yuv420p",
			}

			if vbr {
				// Drop near-identical frames so static screens don't waste bandwidth.
				outputArgs = append(outputArgs, "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2,mpdecimate=max=15")
			}

			useH264 := VideoCodec == "h264" || VideoCodec == "h264_nvenc"

			if useH264 {
				if VideoCodec == "h264_nvenc" {
					outputArgs = append(outputArgs, "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull", "-zerolatency", "1", "-aud", "1")
				} else {
					outputArgs = append(outputArgs, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "aud=1")
				}
			} else {
				outputArgs = append(outputArgs, "-c:v", "libvpx")
			}

			if mode == "bandwidth" {
				bitrateStr := fmt.Sprintf("%dk", bw*1000)
				bufSizeStr := fmt.Sprintf("%dk", bw*200)

				outputArgs = append(outputArgs,
					"-b:v", bitrateStr,
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
				if useH264 {
					outputArgs = append(outputArgs, "-g", fmt.Sprintf("%d", fps*2))
				} else {
					outputArgs = append(outputArgs, "-crf", "20", "-static-thresh", "1000")
				}
			} else {
				if useH264 {
					// H.264 quality mode (CRF)
					crf := 51 - (quality-10)*33/90 // Map 10-100 to 51-18
					outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", crf))
				} else {
					crf := 50 - (quality-10)*46/90
					if crf < 4 {
						crf = 4
					}
					if crf > 63 {
						crf = 63
					}
					outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", crf), "-qmin", fmt.Sprintf("%d", crf))
				}
				maxKbps := 2000 + (quality-10)*18000/90
				maxrateStr := fmt.Sprintf("%dk", maxKbps)
				bufsizeStr := fmt.Sprintf("%dk", maxKbps/5)
				outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
			}

			if useH264 {
				outputArgs = append(outputArgs,
					"-g", fmt.Sprintf("%d", fps*2),
					"-f", "h264",
					"pipe:1",
				)
			} else {
				cpuUsedStr := fmt.Sprintf("%d", cpuEffort)
				outputArgs = append(outputArgs,
					"-lag-in-frames", "0",
					"-error-resilient", "1",
					"-rc_lookahead", "0",
					"-g", fmt.Sprintf("%d", fps),
					"-deadline", "realtime",
					"-cpu-used", cpuUsedStr,
					"-threads", fmt.Sprintf("%d", cpuThreads),
					"-speed", "8",
					"-flush_packets", "1",
					"-f", "ivf",
					"pipe:1",
				)
			}

			log.Printf("Starting ffmpeg capture (%s) from %s at %s target...", VideoCodec, Display, mode)

			args := append([]string{
				"-probesize", "32",
				"-analyzeduration", "0",
				"-fflags", "nobuffer",
				"-threads", "2",
			}, inputArgs...)
			// Add -vsync drop so ffmpeg drops frames when encoder can't keep up
			args = append(args, "-vsync", "drop")
			log.Printf("ffmpeg args: %v", args)
			args = append(args, outputArgs...)

			cmd := exec.Command(ffmpegPath, args...)
			cmd.Env = append(os.Environ(), "DISPLAY="+Display)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Fatalf("Failed to get stdout from ffmpeg: %v", err)
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				log.Fatalf("Failed to get stderr from ffmpeg: %v", err)
			}

			ffmpegMutex.Lock()
			ffmpegStreamID++
			currentStreamID := ffmpegStreamID
			ffmpegCmd = cmd
			ffmpegMutex.Unlock()

			if err := cmd.Start(); err != nil {
				log.Fatalf("Failed to start ffmpeg: %v", err)
			}

			// Log stderr in background
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := stderr.Read(buf)
					if n > 0 {
						log.Printf("[ffmpeg stderr]: %s", string(buf[:n]))
					}
					if err != nil {
						break
					}
				}
			}()

			// Start frame splitting in a bounded way
			doneCh := make(chan struct{})
			go func() {
				if useH264 {
					splitH264AnnexB(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				} else {
					splitIVF(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				}
				close(doneCh)
			}()

			// Wait for splitter to finish reading pipeline to avoid Wait closing stdout prematurely
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

func splitH264AnnexB(reader io.Reader, onFrame func([]byte)) {
	buffer := make([]byte, 0, 1024*1024)
	temp := make([]byte, 16384)
	
	// AUD (Access Unit Delimiter) NAL unit start: 00 00 00 01 09
	// We use this as a frame boundary because we enabled -aud 1 in ffmpeg
	aud := []byte{0x00, 0x00, 0x00, 0x01, 0x09}

	for {
		n, err := reader.Read(temp)
		if n > 0 {
			buffer = append(buffer, temp[:n]...)
			for {
				// Find next AUD, but skip the one at the very beginning of our buffer
				if len(buffer) < 10 {
					break
				}
				
				// Search for next AUD starting from index 5
				nextIdx := -1
				for i := 5; i <= len(buffer)-len(aud); i++ {
					match := true
					for j := 0; j < len(aud); j++ {
						if buffer[i+j] != aud[j] {
							match = false
							break
						}
					}
					if match {
						nextIdx = i
						break
					}
				}

				if nextIdx != -1 {
					frame := make([]byte, nextIdx)
					copy(frame, buffer[:nextIdx])
					onFrame(frame)
					
					// Move remaining data to front
					newBuf := make([]byte, len(buffer)-nextIdx)
					copy(newBuf, buffer[nextIdx:])
					buffer = newBuf
				} else {
					break
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading H264 stream: %v", err)
			}
			// Flush remaining buffer as the last frame
			if len(buffer) > 0 {
				onFrame(buffer)
			}
			return
		}
	}
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
