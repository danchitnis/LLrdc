package server

import (
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

func startAudioStreaming() {
	cleanupTasks = append(cleanupTasks, func() {
		ffmpegMutex.Lock()
		defer ffmpegMutex.Unlock()
		if ffmpegAudioCmd != nil && ffmpegAudioCmd.Process != nil {
			log.Println("Killing audio ffmpeg (cleanup)...")
			ffmpegAudioCmd.Process.Kill()
		}
	})

	go func() {
		for {
			ffmpegMutex.Lock()
			shouldRun := ffmpegShouldRun
			enableAudio := EnableAudio
			audioBitrate := AudioBitrate
			resizing := isResizing
			ffmpegMutex.Unlock()

			if !shouldRun {
				break
			}
			if resizing {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if !enableAudio {
				time.Sleep(2 * time.Second)
				continue
			}

			// Wait for remote.monitor to be available
			env := append(os.Environ(), "XDG_RUNTIME_DIR=/tmp/llrdc-run")

			log.Println("Starting ffmpeg audio capture from remote.monitor...")
			cmd := exec.Command("ffmpeg",
				"-f", "pulse",
				"-i", "remote.monitor",
				"-c:a", "libopus",
				"-b:a", audioBitrate,
				"-page_duration", "20",
				"-flush_packets", "1",
				"-f", "ogg",
				"pipe:1",
			)
			cmd.Env = env
			cmd.Stderr = os.Stderr // for debugging

			ffmpegMutex.Lock()
			ffmpegAudioCmd = cmd
			ffmpegMutex.Unlock()

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				log.Printf("Failed to get audio stdout: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start audio ffmpeg: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			ogg, _, err := oggreader.NewWith(stdout)
			if err != nil {
				log.Printf("Failed to create ogg reader: %v", err)
				cmd.Process.Kill()
				cmd.Wait()
				time.Sleep(5 * time.Second)
				continue
			}

			var lastGranule uint64
			pageCount := 0
			for {
				pageData, pageHeader, err := ogg.ParseNextPage()
				if err != nil {
					log.Printf("Ogg parse error: %v", err)
					break
				}

				pageCount++
				// Do not skip any pages, pass everything to WriteSample
				if UseDebugFFmpeg {
					if pageCount <= 10 {
						log.Printf("Parsed Ogg page %d, data len: %d", pageCount, len(pageData))
					} else if pageCount%50 == 0 {
						log.Printf("Parsed %d Ogg pages, data len: %d", pageCount, len(pageData))
					}
				}

				sampleCount := float64(pageHeader.GranulePosition - lastGranule)
				lastGranule = pageHeader.GranulePosition
				sampleDuration := time.Duration((sampleCount/48000)*1000) * time.Millisecond
				if sampleDuration == 0 {
					sampleDuration = 20 * time.Millisecond
				}

				videoTrackMutex.RLock()
				at := audioTrack
				videoTrackMutex.RUnlock()

				if at != nil {
					_ = at.WriteSample(media.Sample{
						Data:     pageData,
						Duration: sampleDuration,
					})
				}
			}

			cmd.Wait()
			time.Sleep(2 * time.Second)
		}
	}()
}
