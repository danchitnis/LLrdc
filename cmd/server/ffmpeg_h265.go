package main

import (
	"fmt"
	"io"
	"log"
)

func buildH265Args(mode string, bw int, quality int, fps int, vbr bool, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "h265_nvenc" {
	        outputArgs = append(outputArgs, "-c:v", "hevc_nvenc", "-preset", "p1", "-tune", "ll", "-aud", "1")
			if Chroma == "444" {
				outputArgs = append(outputArgs, "-profile:v", "rext")
			}
	} else {
			x265Params := fmt.Sprintf("aud=1:fps=%d", fps)
	        outputArgs = append(outputArgs, "-c:v", "libx265", "-preset", "ultrafast", "-tune", "zerolatency", "-x265-params", x265Params)
			if Chroma == "444" {
				outputArgs = append(outputArgs, "-profile:v", "main444-8")
			}
	}
	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			if VideoCodec == "h265_nvenc" {
				outputArgs = append(outputArgs,
					"-rc", "vbr",
					"-cq", "30",
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			} else {
				outputArgs = append(outputArgs,
					"-crf", "30",
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			}
		} else {
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
			)
			if VideoCodec == "h265_nvenc" {
				outputArgs = append(outputArgs, "-rc", "cbr")
			}
		}
	} else {
		val := 51 - (quality-10)*33/90 // Map 10-100 to 51-18
		if VideoCodec == "h265_nvenc" {
			outputArgs = append(outputArgs, "-rc", "vbr", "-cq", fmt.Sprintf("%d", val))
		} else {
			outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", val))
		}

		maxKbps := 2000 + (quality-10)*18000/90
		maxrateStr := fmt.Sprintf("%dk", maxKbps)
		bufsizeStr := fmt.Sprintf("%dk", maxKbps*2)
		outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
	}

	if !vbr {
		outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))
	}

	outputArgs = append(outputArgs,
		"-max_muxing_queue_size", "1024",
		"-g", fmt.Sprintf("%d", fps*keyframeInterval),
		"-f", "hevc",		"pipe:1",
	)

	return outputArgs
}

func splitH265AnnexB(reader io.Reader, onFrame func([]byte)) {
	buffer := make([]byte, 0, 1024*1024)
	temp := make([]byte, 16384)

	for {
		n, err := reader.Read(temp)
		if n > 0 {
			buffer = append(buffer, temp[:n]...)
			for {
				if len(buffer) < 10 {
					break
				}

				nextIdx := -1
				// Skip the first few bytes assuming they might be the start of the current frame
				for i := 4; i <= len(buffer)-6; i++ {
					if buffer[i] == 0x00 && buffer[i+1] == 0x00 && buffer[i+2] == 0x00 && buffer[i+3] == 0x01 {
						nalType := (buffer[i+4] & 0x7E) >> 1
						if nalType == 35 { // AUD NAL Unit in H.265 is 35
							nextIdx = i
							break
						}
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
				log.Printf("Error reading H265 stream: %v", err)
			}
			if len(buffer) > 0 {
				onFrame(buffer)
			}
			return
		}
	}
}
