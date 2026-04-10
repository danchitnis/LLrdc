package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

var (
	targetMode             = "bandwidth" // "bandwidth" or "quality"
	targetBandwidthMbps    = 5           // Initial default: 5 Mbps
	targetQuality          = 70          // 10-100
	targetVBR              = false       // Default VBR to false
	targetVBRThreshold     = 0           // Default VBR threshold to 0
	targetDamageTracking   = false       // Default Damage Tracking to false
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
	lastFFmpegRestartTime  atomic.Pointer[time.Time]
	isResizing             = false
)

func getLastFFmpegRestartTime() time.Time {
	t := lastFFmpegRestartTime.Load()
	if t == nil {
		return time.Time{}
	}
	return *t
}

func killFFmpegWithTimestamp() {
	now := time.Now()
	lastFFmpegRestartTime.Store(&now)
	if ffmpegCmd != nil && ffmpegCmd.Process != nil {
		_ = ffmpegCmd.Process.Kill()
	}
}

func PauseStreaming() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()
	isResizing = true
	log.Println("Pausing wf-recorder for resize...")
	killFFmpegWithTimestamp()
	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Println("Pausing audio ffmpeg for resize...")
		ffmpegAudioCmd.Process.Kill()
	}
}

func ResumeStreaming() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()
	isResizing = false
}

func isQSVCodec(codec string) bool {
	return codec == "h264_qsv" || codec == "h265_qsv" || codec == "av1_qsv" || codec == "h264_vaapi" || codec == "hevc_vaapi" || codec == "av1_vaapi"
}

func SetChroma(chroma string) {
	if chroma != "420" && chroma != "444" {
		log.Printf("Invalid chroma setting: %s", chroma)
		return
	}
	if CaptureMode == CaptureModeDirect && chroma != "420" {
		log.Printf("Ignoring chroma change to %s: direct capture mode currently requires 420", chroma)
		return
	}

	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if Chroma == chroma {
		return
	}

	Chroma = chroma
	log.Printf("Received chroma config: %s", chroma)

	killFFmpegWithTimestamp()
}

func SetVideoCodec(codec string) {
	if codec != "vp8" && codec != "h264" && codec != "h264_nvenc" && codec != "h264_qsv" && codec != "h265" && codec != "h265_nvenc" && codec != "h265_qsv" && codec != "av1" && codec != "av1_nvenc" && codec != "av1_qsv" {
		log.Printf("Invalid video codec: %s", codec)
		return
	}
	if CaptureMode == CaptureModeDirect && !isNVENCCodec(codec) {
		log.Printf("Ignoring codec change to %s: direct capture mode requires NVENC", codec)
		return
	}

	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if VideoCodec == codec {
		return
	}

	VideoCodec = codec
	log.Printf("Target video codec changed to %s, reinitializing WebRTC track and restarting ffmpeg...", codec)

	initWebRTCTrack() // Re-create track

	killFFmpegWithTimestamp()
}

func SetKeyframeInterval(interval int) {
	if interval < 1 {
		interval = 1
	} else if interval > 10 {
		interval = 10
	}
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetKeyframeInterval == interval {
		return
	}

	targetKeyframeInterval = interval

	log.Printf("Received keyframe interval config: %d", interval)
	killFFmpegWithTimestamp()
}

func SetMpdecimate(mpdecimate bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetMpdecimate == mpdecimate {
		return
	}

	targetMpdecimate = mpdecimate

	log.Printf("Received mpdecimate config: %v", mpdecimate)
	killFFmpegWithTimestamp()
}

func SetCpuEffort(effort int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetCpuEffort == effort {
		return
	}

	targetCpuEffort = effort

	log.Printf("Received CPU effort config: %d", effort)
	killFFmpegWithTimestamp()
}

func SetCpuThreads(threads int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetCpuThreads == threads {
		return
	}

	targetCpuThreads = threads

	log.Printf("Received CPU threads config: %d", threads)
	killFFmpegWithTimestamp()
}

func SetDrawMouse(draw bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetDrawMouse == draw {
		return
	}

	targetDrawMouse = draw

	log.Printf("Received Enable Desktop Mouse config: %v", draw)
	killFFmpegWithTimestamp()
}

func SetVBR(vbr bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetVBR == vbr {
		return
	}

	targetVBR = vbr

	log.Printf("Received VBR config: %v", vbr)
	killFFmpegWithTimestamp()
}

func SetVBRThreshold(threshold int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetVBRThreshold == threshold {
		return
	}

	targetVBRThreshold = threshold

	log.Printf("Received VBR Threshold config: %d", threshold)
	killFFmpegWithTimestamp()
}

func SetDamageTracking(dt bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetDamageTracking == dt {
		return
	}

	targetDamageTracking = dt

	log.Printf("Received Damage Tracking config: %v", dt)
	killFFmpegWithTimestamp()
}

func SetBandwidth(bwMbps int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetMode == "bandwidth" && targetBandwidthMbps == bwMbps {
		return
	}

	targetMode = "bandwidth"
	targetBandwidthMbps = bwMbps

	log.Printf("Received bandwidth config: %d Mbps", bwMbps)
	killFFmpegWithTimestamp()
}

func SetQuality(quality int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if targetMode == "quality" && targetQuality == quality {
		return
	}

	targetMode = "quality"
	targetQuality = quality

	log.Printf("Received quality config: %d", quality)
	killFFmpegWithTimestamp()
}

func SetFramerate(fps int) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if FPS == fps {
		return
	}

	FPS = fps

	log.Printf("Received framerate config: %d fps", fps)
	killFFmpegWithTimestamp()
}

func RestartForResize() {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	log.Println("Screen size changed, restarting ffmpeg...")
	killFFmpegWithTimestamp()

	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Println("Screen size changed, restarting audio ffmpeg...")
		ffmpegAudioCmd.Process.Kill()
	}
}

func SetEnableAudio(enable bool) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if EnableAudio == enable {
		return
	}

	EnableAudio = enable

	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Printf("Enable audio changed to %v, restarting audio ffmpeg...", enable)
		ffmpegAudioCmd.Process.Kill()
	}
}

func SetAudioBitrate(bitrate string) {
	ffmpegMutex.Lock()
	defer ffmpegMutex.Unlock()

	if AudioBitrate == bitrate {
		return
	}

	AudioBitrate = bitrate

	if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
		log.Printf("Audio bitrate changed to %s, restarting audio ffmpeg...", bitrate)
		_ = ffmpegAudioCmd.Process.Kill()
	}
}

func getIntelDRMNode() string {
	if _, err := os.Stat("/dev/dri/renderD129"); err == nil {
		return "/dev/dri/renderD129"
	}
	return "/dev/dri/renderD128"
}

func startStreaming(onFrame func([]byte, uint32, string)) {
	var lastStreamID uint32

	cleanupTasks = append(cleanupTasks, func() {
		ffmpegMutex.Lock()
		defer ffmpegMutex.Unlock()
		ffmpegShouldRun = false
		log.Println("Killing wf-recorder (cleanup)...")
		killFFmpegWithTimestamp()
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

			if err := validateRuntimeDirectMode(VideoCodec, Chroma); err != nil {
				setDirectBufferActive(false, err.Error())
				log.Fatalf("Invalid direct-buffer runtime configuration: %v", err)
			}

			codec := "libvpx"
			codecName := "vp8"
			format := "ivf"
			if VideoCodec == "h264" {
				codec = "libx264"
				codecName = "h264"
				format = "h264"
			} else if VideoCodec == "h264_nvenc" {
				codec = "h264_nvenc"
				codecName = "h264"
				format = "h264"
			} else if VideoCodec == "h264_qsv" {
				codec = "h264_vaapi"
				codecName = "h264"
				format = "h264"
			} else if VideoCodec == "h265" {
				codec = "libx265"
				codecName = "h265"
				format = "hevc"
			} else if VideoCodec == "h265_nvenc" {
				codec = "hevc_nvenc"
				codecName = "h265"
				format = "hevc"
			} else if VideoCodec == "h265_qsv" {
				codec = "hevc_vaapi"
				codecName = "h265"
				format = "hevc"
			} else if VideoCodec == "av1" {
				codec = "libaom-av1"
				codecName = "av1"
				format = "ivf"
			} else if VideoCodec == "av1_nvenc" {
				codec = "av1_nvenc"
				codecName = "av1"
				format = "ivf"
			} else if VideoCodec == "av1_qsv" {
				codec = "av1_vaapi"
				codecName = "av1"
				format = "ivf"
			}



			var cmd *exec.Cmd
			if TestPattern {
				setDirectBufferActive(false, "Direct buffer is unavailable in test-pattern mode")
				log.Printf("TEST_PATTERN mode: starting ffmpeg with testsrc")
				w, h := GetScreenSize()
				var ffmpegArgs []string
				if VideoCodec == "vp8" {
					ffmpegArgs = buildVP8Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetCpuEffort, targetCpuThreads, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "h264" || VideoCodec == "h264_nvenc" {
					ffmpegArgs = buildH264Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "h264_qsv" {
					ffmpegArgs = buildQSVH264Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "h265" || VideoCodec == "h265_nvenc" {
					ffmpegArgs = buildH265Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "h265_qsv" {
					ffmpegArgs = buildQSVH265Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "av1" || VideoCodec == "av1_nvenc" {
					ffmpegArgs = buildAV1Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				} else if VideoCodec == "av1_qsv" {
					ffmpegArgs = buildQSVAV1Args(targetMode, targetBandwidthMbps, targetQuality, FPS, targetVBR, targetVBRThreshold, targetKeyframeInterval)
				}

				// Insert testsrc at the beginning
				finalArgs := append([]string{"-y", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%dx%d:rate=%d", w, h, FPS)}, ffmpegArgs...)
				log.Printf("FFmpeg testsrc command: ffmpeg %v", finalArgs)
				cmd = exec.Command("ffmpeg", finalArgs...)
			} else {
				// Base config using wf-recorder
				args := []string{
					"-o0", "wf-recorder",
				}

				if !targetDamageTracking {
					args = append(args, "-D") // Disable damage tracking to continuously emit frames
				}

				args = append(args,
					"-c", codec,
					"-m", format,
					"-r", fmt.Sprintf("%d", FPS),
				)

				if codec == "h264_nvenc" || codec == "hevc_nvenc" || codec == "av1_nvenc" {
					// Always use packed RGB/BGR and let NVENC convert to YUV on-GPU.
					// Forcing yuv420p here pushes the conversion into wf-recorder/FFmpeg's
					// CPU path and is very expensive at 4K60.
					args = append(args, "-x", "bgr0")

					// NVENC hardware encoding
					args = append(args,
						"-p", "preset=p1",
						"-p", "tune=ull",
						"-p", "delay=0",
						"-p", "surfaces=64",
						"-p", "rgb_mode=yuv420",
						"-p", "bf=0",
						"-p", "spatial-aq=0",
						"-p", "temporal-aq=0",
						"-p", "strict_gop=1",
						"-p", fmt.Sprintf("b=%dM", targetBandwidthMbps),
						"-p", fmt.Sprintf("maxrate=%dM", targetBandwidthMbps),
						"-p", fmt.Sprintf("bufsize=%dM", targetBandwidthMbps*2),
						"-p", fmt.Sprintf("g=%d", targetKeyframeInterval*FPS),
					)
					if NVENCLatencyMode {
						args = append(args, "-p", "rc-lookahead=0", "-p", "no-scenecut=1", "-p", "b_ref_mode=0")
					}
					if !targetVBR {
						args = append(args, "-p", "rc=cbr")
					} else {
						args = append(args, "-p", "rc=vbr", "-p", "cq=30")
					}

					if codec == "h264_nvenc" || codec == "hevc_nvenc" {
						args = append(args, "-p", "aud=1")
					}
				} else if isQSVCodec(codec) {
					// Intel hardware encoding via VAAPI (mapped from QSV names internally)
					args = append(args, "-d", getIntelDRMNode())
					
					// We do not pass -x nv12 here because wf-recorder automatically 
					// adds the necessary hwupload and scale_vaapi=format=nv12 filters.

					args = append(args,
						"-p", fmt.Sprintf("b=%dk", targetBandwidthMbps*1000),
						"-p", fmt.Sprintf("maxrate=%dk", targetBandwidthMbps*1000),
						"-p", fmt.Sprintf("g=%d", targetKeyframeInterval*FPS),
					)

					if codec == "h264_vaapi" || codec == "hevc_vaapi" {
						args = append(args, "-p", "aud=1")
					}
					
					if codec == "h264_vaapi" {
						args = append(args, "-p", "profile=77") // main
					} else if codec == "hevc_vaapi" {
						args = append(args, "-p", "profile=1") // main
					} else if codec == "av1_vaapi" {
						args = append(args, "-p", "profile=0") // main
					}
				} else {
					// CPU encoding
					args = append(args, "-x", "yuv420p")

					if codec == "libvpx" {
						args = append(args,
							"-p", "deadline=realtime",
							"-p", "lag-in-frames=0",
						)
						if targetVBR {
							// For VBR: set a target maxrate and use static-thresh for bit saving
							args = append(args, "-p", fmt.Sprintf("static-thresh=%d", targetVBRThreshold), "-p", "crf=30", "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						} else {
							// For CBR: target, maxrate, and minrate should match
							args = append(args, "-p", "static-thresh=0", "-p", fmt.Sprintf("minrate=%dM", targetBandwidthMbps), "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						}
					} else if codec == "libx264" || codec == "libx265" {
						args = append(args, "-p", "tune=zerolatency")
						if targetVBR {
							// For H264/H265 VBR, use threshold to offset CRF
							crf := 28 + (targetVBRThreshold / 50)
							if crf > 51 { crf = 51 }
							args = append(args, "-p", fmt.Sprintf("crf=%d", crf), "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						} else {
							args = append(args, "-p", fmt.Sprintf("minrate=%dM", targetBandwidthMbps), "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						}

						if codec == "libx264" {
							args = append(args, "-p", fmt.Sprintf("x264-params=aud=1:fps=%d", FPS))
						} else {
							args = append(args, "-p", fmt.Sprintf("x265-params=aud=1:fps=%d", FPS))
						}
					} else if codec == "libaom-av1" {
						args = append(args, "-p", "usage=realtime", "-p", "row-mt=1", "-p", "lag-in-frames=0", "-p", "error-resilient=1")
						if targetVBR {
							args = append(args, "-p", fmt.Sprintf("static-thresh=%d", targetVBRThreshold), "-p", "crf=35", "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						} else {
							args = append(args, "-p", "static-thresh=0", "-p", fmt.Sprintf("minrate=%dM", targetBandwidthMbps), "-p", fmt.Sprintf("b=%dM", targetBandwidthMbps))
						}
					}

					args = append(args,
						"-p", "cpu-used=8",
						"-p", fmt.Sprintf("threads=%d", targetCpuThreads),
						"-p", fmt.Sprintf("maxrate=%dM", targetBandwidthMbps),
						"-p", fmt.Sprintf("g=%d", targetKeyframeInterval*FPS),
					)
				}

				args = append(args, "-f", "pipe:1")

				log.Printf("Starting wf-recorder capture: %v", args)
				cmd = exec.Command("stdbuf", append([]string{"-i0", "-o0"}, args[1:]...)...)
				cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/tmp/llrdc-run")
			}

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
			noteStreamStarted(currentStreamID)

			if err := cmd.Start(); err != nil {
				if CaptureMode == CaptureModeDirect {
					setDirectBufferActive(false, fmt.Sprintf("Direct-buffer capture failed to start: %v", err))
				}
				log.Fatalf("Failed to start wf-recorder: %v", err)
			}
			if CaptureMode == CaptureModeDirect {
				setDirectBufferActive(true, "Direct-buffer probe passed and NVENC capture is active")
			}

			// Prime the compositor so damage tracking sessions emit an initial frame without waiting for user input.
			PrimeFrameGeneration(0, 10, 100*time.Millisecond)

			doneCh := make(chan bool, 1)
			go func() {
				streamProducedFrame := false
				onFrameWithCheck := func(frame []byte, sid uint32) {
					if sid != lastStreamID {
						log.Printf("Stream ID change detected: %d -> %d. Triggering config reset.", lastStreamID, sid)
						noteStreamFrame(sid)
						streamProducedFrame = true
						broadcastConfig(true)
						lastStreamID = sid
					} else if !streamProducedFrame {
						noteStreamFrame(sid)
						streamProducedFrame = true
					}
					onFrame(frame, sid, codecName)
				}

				if format == "ivf" {
					splitIVF(stdout, func(frame []byte) {
						onFrameWithCheck(frame, currentStreamID)
					})
				} else if format == "h264" {
					splitH264AnnexB(stdout, func(frame []byte) {
						onFrameWithCheck(frame, currentStreamID)
					})
				} else if format == "hevc" {
					splitH265AnnexB(stdout, func(frame []byte) {
						onFrameWithCheck(frame, currentStreamID)
					})
				} else {
					log.Printf("Unknown format: %s, defaulting to splitIVF", format)
					splitIVF(stdout, func(frame []byte) {
						onFrameWithCheck(frame, currentStreamID)
					})
				}
				doneCh <- streamProducedFrame
			}()

			streamProducedFrame := <-doneCh
			_ = cmd.Wait()

			ffmpegMutex.Lock()
			shouldRun := ffmpegShouldRun
			ffmpegMutex.Unlock()

			if !shouldRun {
				break
			}
			if !streamProducedFrame {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()
}
