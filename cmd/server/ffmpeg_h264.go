package main

import (
	"fmt"
	"io"
	"log"
)

func buildH264Args(mode string, bw int, quality int, fps int, vbr bool, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "h264_nvenc" {
	        outputArgs = append(outputArgs, "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull", "-aud", "1", "-level", "6.0")
			if Chroma == "444" {
				outputArgs = append(outputArgs, "-profile:v", "high444p")
			}
	} else {
			x264Params := fmt.Sprintf("aud=1:fps=%d", fps)
	        outputArgs = append(outputArgs, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", x264Params, "-level", "6.0")
			if Chroma == "444" {
				outputArgs = append(outputArgs, "-profile:v", "high444")
			}
	}
	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		// Use a 2 second buffer (bw*2000) to prevent VBV underflows at high framerates with large I-frames
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			if VideoCodec == "h264_nvenc" {
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
			if VideoCodec == "h264_nvenc" {
				outputArgs = append(outputArgs, "-rc", "cbr")
			}
		}
	} else {
		val := 51 - (quality-10)*33/90 // Map 10-100 to 51-18
		if VideoCodec == "h264_nvenc" {
			outputArgs = append(outputArgs, "-rc", "vbr", "-cq", fmt.Sprintf("%d", val))
		} else {
			outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", val))
		}

		maxKbps := 2000 + (quality-10)*18000/90
		// Use a 2 second buffer
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
		"-f", "h264",		"pipe:1",
	)

	return outputArgs
}

func splitH264AnnexB(reader io.Reader, onFrame func([]byte)) {
	buffer := make([]byte, 0, 1024*1024)
	temp := make([]byte, 16384)

	for {
		n, err := reader.Read(temp)
		if n > 0 {
			buffer = append(buffer, temp[:n]...)
			for {
				if len(buffer) < 5 {
					break
				}

				nextIdx := -1
				// Look for either 00 00 00 01 09 or 00 00 01 09
				for i := 4; i <= len(buffer)-4; i++ {
					if buffer[i] == 0x09 {
						if i >= 4 && buffer[i-1] == 0x01 && buffer[i-2] == 0x00 && buffer[i-3] == 0x00 && buffer[i-4] == 0x00 {
							nextIdx = i - 4
							break
						} else if i >= 3 && buffer[i-1] == 0x01 && buffer[i-2] == 0x00 && buffer[i-3] == 0x00 {
							nextIdx = i - 3
							break
						}
					}
				}

				if nextIdx > 0 {
					frame := make([]byte, nextIdx)
					copy(frame, buffer[:nextIdx])
					onFrame(frame)

					newBuf := make([]byte, len(buffer)-nextIdx)
					copy(newBuf, buffer[nextIdx:])
					buffer = newBuf
				} else if nextIdx == 0 {
					// We are exactly at a start code. We need to find the NEXT start code to slice the frame.
					endIdx := -1
					for i := 5; i <= len(buffer)-4; i++ {
						if buffer[i] == 0x09 {
							if i >= 4 && buffer[i-1] == 0x01 && buffer[i-2] == 0x00 && buffer[i-3] == 0x00 && buffer[i-4] == 0x00 {
								endIdx = i - 4
								break
							} else if i >= 3 && buffer[i-1] == 0x01 && buffer[i-2] == 0x00 && buffer[i-3] == 0x00 {
								endIdx = i - 3
								break
							}
						}
					}

					if endIdx != -1 {
						frame := make([]byte, endIdx)
						copy(frame, buffer[:endIdx])
						onFrame(frame)

						newBuf := make([]byte, len(buffer)-endIdx)
						copy(newBuf, buffer[endIdx:])
						buffer = newBuf
					} else {
						break
					}
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
