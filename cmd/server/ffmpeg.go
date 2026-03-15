package main

import (
	"fmt"
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
	targetVBR              = true        // Default VBR to true
	targetMpdecimate       = false       // Default mpdecimate to false
	targetCpuEffort        = 6           // Default: 6
	targetCpuThreads       = 4           // Default: 4
	targetDrawMouse        = true        // Default: true
	targetKeyframeInterval = 2           // Default: 2 seconds
	ffmpegCmd              *exec.Cmd
	ffmpegMutex            sync.Mutex
	ffmpegShouldRun        = true
	ffmpegStreamID         uint32
)

func SetChroma(chroma string) {
	if chroma != "420" && chroma != "444" {
		log.Printf("Invalid chroma setting: %s", chroma)
		return
	}

	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	Chroma = chroma
	log.Printf("Target chroma changed to %s, restarting ffmpeg...", chroma)

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		ffmpegCmd.Process.Kill()
	}
}

func SetVideoCodec(codec string) {
	if codec != "vp8" && codec != "h264" && codec != "h264_nvenc" && codec != "h265" && codec != "h265_nvenc" && codec != "av1" && codec != "av1_nvenc" {
		log.Printf("Invalid video codec: %s", codec)
		return
	}

	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	VideoCodec = codec
	log.Printf("Target video codec changed to %s, reinitializing WebRTC track and restarting ffmpeg...", codec)
	
	initWebRTCTrack() // Re-create track

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		ffmpegCmd.Process.Kill()
	}
}

func SetKeyframeInterval(interval int) {
	if interval < 1 {
		interval = 1
	} else if interval > 10 {
		interval = 10
	}
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetKeyframeInterval = interval

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target keyframe interval changed to %d, restarting ffmpeg...", interval)
		ffmpegCmd.Process.Kill()
	}
}

func SetMpdecimate(mpdecimate bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	targetMpdecimate = mpdecimate

	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Printf("Target mpdecimate changed to %v, restarting ffmpeg...", mpdecimate)
		ffmpegCmd.Process.Kill()
	}
}

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
			mpdecimate := targetMpdecimate
			cpuEffort := targetCpuEffort
			cpuThreads := targetCpuThreads
			drawMouse := targetDrawMouse
			keyframeInterval := targetKeyframeInterval
			ffmpegMutex.Unlock()

			width, height := GetScreenSize()
			size := fmt.Sprintf("%dx%d", width, height)

			drawMouseStr := "0"
			if drawMouse {
				drawMouseStr = "1"
			}

			inputArgs := []string{"-framerate", fmt.Sprintf("%d", fps), "-f", "x11grab", "-draw_mouse", drawMouseStr, "-video_size", size, "-i", Display + ".0"}
			if TestPattern {
				inputArgs = []string{"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%s:rate=%d", size, fps)}
			}

			useNVENC := VideoCodec == "h264_nvenc" || VideoCodec == "h265_nvenc" || VideoCodec == "av1_nvenc"
			
			var filterStr string
			if mpdecimate {
				filterStr = "mpdecimate=max=15,settb=1/1000"
			} else {
				filterStr = "settb=1/1000"
			}

			outputArgs := []string{}
			if useNVENC {
				if filterStr != "" {
					filterStr += ","
				}
				// For NVENC, ensure even dimensions on CPU, then upload to GPU.
				if Chroma == "444" {
					// CPU-side format=yuv444p is required because:
					// 1. NVENC won't auto-convert BGR0→YUV444p even with high444p profile
					// 2. scale_cuda doesn't support rgb0→yuv444p conversion
					// This does increase CPU usage at high resolutions (~50-85%).
					filterStr += "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv444p,hwupload_cuda"
				} else {
					filterStr += "scale=trunc(iw/2)*2:trunc(ih/2)*2,hwupload_cuda"
				}
				outputArgs = append(outputArgs, "-vf", filterStr)
			} else {
				if filterStr != "" {
					filterStr += ","
				}
				if Chroma == "444" {
					filterStr += "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv444p"
				} else {
					filterStr += "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p"
				}
				outputArgs = append(outputArgs, "-vf", filterStr)
			}

			useH264 := VideoCodec == "h264" || VideoCodec == "h264_nvenc"
			useH265 := VideoCodec == "h265" || VideoCodec == "h265_nvenc"
			useAV1 := VideoCodec == "av1" || VideoCodec == "av1_nvenc"

			if useH264 {
				outputArgs = append(outputArgs, buildH264Args(mode, bw, quality, fps, vbr, keyframeInterval)...)
			} else if useH265 {
				outputArgs = append(outputArgs, buildH265Args(mode, bw, quality, fps, vbr, keyframeInterval)...)
			} else if useAV1 {
				outputArgs = append(outputArgs, buildAV1Args(mode, bw, quality, fps, vbr, keyframeInterval)...)
			} else {
				outputArgs = append(outputArgs, buildVP8Args(mode, bw, quality, fps, cpuEffort, cpuThreads, vbr, keyframeInterval)...)
			}

			log.Printf("Starting ffmpeg capture (%s) from %s at %s target...", VideoCodec, Display, mode)

			initialArgs := []string{
				"-probesize", "32",
				"-analyzeduration", "0",
				"-fflags", "nobuffer",
				"-threads", "2",
			}
			if !UseDebugFFmpeg {
				initialArgs = append(initialArgs, "-nostats")
			}
			if useNVENC {
				initialArgs = append(initialArgs, "-init_hw_device", "cuda=cu:0", "-filter_hw_device", "cu")
			}

			args := append(initialArgs, inputArgs...)
			if vbr {
				args = append(args, "-fps_mode", "vfr")
			}
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
				} else if useH265 {
					splitH265AnnexB(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				} else {
					// Both VP8 and AV1 use IVF splitter
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
