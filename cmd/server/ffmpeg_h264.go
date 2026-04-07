package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
)

func buildH264Args(mode string, bw int, quality int, fps int, vbr bool, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "h264_nvenc" {
		outputArgs = append(outputArgs, "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull", "-delay", "0", "-surfaces", "64", "-bf", "0", "-spatial-aq", "0", "-temporal-aq", "0", "-strict_gop", "1", "-aud", "1", "-level", "6.0")
		if NVENCLatencyMode {
			outputArgs = append(outputArgs, "-rc-lookahead", "0", "-no-scenecut", "1", "-b_ref_mode", "0")
		}
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

	outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))

	outputArgs = append(outputArgs,
		"-max_muxing_queue_size", "1024",
		"-g", fmt.Sprintf("%d", fps*keyframeInterval),
		"-f", "h264", "pipe:1",
	)

	return outputArgs
}

func splitH264AnnexB(reader io.Reader, onFrame func([]byte)) {
	buffer := make([]byte, 0, 1024*1024)
	temp := make([]byte, 16384)
	marker4 := []byte{0x00, 0x00, 0x00, 0x01, 0x09}
	marker3 := []byte{0x00, 0x00, 0x01, 0x09}

	for {
		n, err := reader.Read(temp)
		if n > 0 {
			buffer = append(buffer, temp[:n]...)
			for {
				if len(buffer) < 5 {
					break
				}

				// Find next AUD marker, skipping the first 4 bytes (the current frame's start)
				nextIdx := -1
				m4Idx := bytes.Index(buffer[4:], marker4)
				m3Idx := bytes.Index(buffer[4:], marker3)

				if m4Idx != -1 && (m3Idx == -1 || m4Idx <= m3Idx) {
					nextIdx = m4Idx + 4
				} else if m3Idx != -1 {
					nextIdx = m3Idx + 4
				}

				if nextIdx != -1 {
					frame := make([]byte, nextIdx)
					copy(frame, buffer[:nextIdx])
					onFrame(frame)
					buffer = buffer[nextIdx:]
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
