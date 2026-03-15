package main

import (
	"log"
	"os/exec"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

func startAudioStreaming() {
	go func() {
		for {
			ffmpegMutex.Lock()
			shouldRun := ffmpegShouldRun
			ffmpegMutex.Unlock()
			if !shouldRun {
				break
			}

			log.Println("Starting ffmpeg audio capture...")
			cmd := exec.Command("ffmpeg",
				"-f", "pulse",
				"-i", "default",
				"-c:a", "libopus",
				"-b:a", "128k",
				"-page_duration", "20",
				"-f", "ogg",
				"pipe:1",
			)

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
			for {
				pageData, pageHeader, err := ogg.ParseNextPage()
				if err != nil {
					break
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
