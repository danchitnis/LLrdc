package main

import (
	"fmt"
	"io"
	"log"
)

func buildH264Args(mode string, bw int, quality int, fps int, vbr bool) []string {
	var outputArgs []string

	if VideoCodec == "h264_nvenc" {
		outputArgs = append(outputArgs, "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull", "-zerolatency", "1", "-aud", "1")
	} else {
		outputArgs = append(outputArgs, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "aud=1")
	}

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*200)

		outputArgs = append(outputArgs,
			"-b:v", bitrateStr,
			"-maxrate", bitrateStr,
			"-bufsize", bufSizeStr,
			"-g", fmt.Sprintf("%d", fps*2),
		)
	} else {
		crf := 51 - (quality-10)*33/90 // Map 10-100 to 51-18
		outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", crf))
		maxKbps := 2000 + (quality-10)*18000/90
		maxrateStr := fmt.Sprintf("%dk", maxKbps)
		bufsizeStr := fmt.Sprintf("%dk", maxKbps/5)
		outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
	}

	outputArgs = append(outputArgs,
		"-g", fmt.Sprintf("%d", fps*2),
		"-f", "h264",
		"pipe:1",
	)

	return outputArgs
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
				if len(buffer) < 10 {
					break
				}

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
			if len(buffer) > 0 {
				onFrame(buffer)
			}
			return
		}
	}
}
