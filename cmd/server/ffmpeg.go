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
	targetMode             = "bandwidth" // "bandwidth" or "quality"
	targetBandwidthMbps    = 5           // Initial default: 5 Mbps
	targetQuality          = 70          // 10-100
	targetVBR              = true        // Default VBR to true
	targetMpdecimate       = false       // Default mpdecimate to false
	targetCpuEffort        = 6           // Default: 6
	targetCpuThreads       = 4           // Default: 4
	targetDrawMouse        = true        // Default: true
	targetKeyframeInterval = 2           // Default: 2 seconds
	ffmpegCmd              *exec.Cmd
	ffmpegAudioCmd         *exec.Cmd
	ffmpegMutex            sync.Mutex
	ffmpegShouldRun        = true
	ffmpegStreamID         uint32
	isResizing             = false
)

func PauseStreaming() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()
	isResizing = true
	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		log.Println("Pausing wf-recorder for resize...")
		ffmpegCmd.Process.Kill()
	}
}

func ResumeStreaming() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()
	isResizing = false
}

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

func SetEnableAudio(enable bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	EnableAudio = enable

	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Printf("Enable audio changed to %v, restarting audio ffmpeg...", enable)
		ffmpegAudioCmd.Process.Kill()
	}
}

func SetAudioBitrate(bitrate string) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	AudioBitrate = bitrate

	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Printf("Audio bitrate changed to %s, restarting audio ffmpeg...", bitrate)
		ffmpegAudioCmd.Process.Kill()
	}
}

func startStreaming(onFrame func([]byte, uint32)) {
	cleanupTasks = append(cleanupTasks, func() {
		ffmpegMutex.Lock()
		defer ffmpegMutex.Unlock()
		ffmpegShouldRun = false
		if ffmpegCmd != nil && ffmpegCmd.Process != nil {
			log.Println("Killing wf-recorder (cleanup)...")
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
			resizing := isResizing
			ffmpegMutex.Unlock()

			if resizing {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			codec := "libvpx"
			format := "ivf"
			if VideoCodec == "h264" || VideoCodec == "h264_nvenc" {
				codec = "libx264"
				format = "h264"
			} else if VideoCodec == "h265" || VideoCodec == "h265_nvenc" {
				codec = "libx265"
				format = "hevc"
			} else if VideoCodec == "av1" || VideoCodec == "av1_nvenc" {
				codec = "libaom-av1"
				format = "ivf"
			}

			// Hardcoded minimal config using wf-recorder
			args := []string{
				"-o0", "wf-recorder",
				"-D", // Disable damage tracking to continuously emit frames
				"-c", codec,
				"-m", format,
				"-x", "yuv420p",
				"-r", fmt.Sprintf("%d", FPS),
				"-p", "deadline=realtime",
				"-p", fmt.Sprintf("cpu-used=%d", targetCpuEffort),
				"-p", fmt.Sprintf("threads=%d", targetCpuThreads),
				"-p", fmt.Sprintf("b=%dM", targetBandwidthMbps), // Target bitrate
				"-p", fmt.Sprintf("maxrate=%dM", targetBandwidthMbps), // Max bitrate
				"-p", "static-thresh=0", // Reduce shimmering
				"-p", "lag-in-frames=0", // Lowest latency
				"-f", "pipe:1",
			}

			log.Printf("Starting wf-recorder capture (stdbuf pipe:1)...")
			cmd := exec.Command("stdbuf", args...)
			cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY=wayland-0")

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Fatalf("Failed to get stdout from wf-recorder: %v", err)
			}
			cmd.Stderr = os.Stderr

			ffmpegMutex.Lock()
			ffmpegStreamID++
			currentStreamID := ffmpegStreamID
			ffmpegCmd = cmd
			ffmpegMutex.Unlock()

			if err := cmd.Start(); err != nil {
				log.Fatalf("Failed to start wf-recorder: %v", err)
			}

			doneCh := make(chan struct{})
			go func() {
				if format == "ivf" {
					splitIVF(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				} else if format == "h264" {
					splitH264AnnexB(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				} else if format == "hevc" {
					splitH265AnnexB(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				} else {
					log.Printf("Unknown format: %s, defaulting to splitIVF", format)
					splitIVF(stdout, func(frame []byte) {
						onFrame(frame, currentStreamID)
					})
				}
				close(doneCh)
			}()

			<-doneCh
			_ = cmd.Wait()

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
