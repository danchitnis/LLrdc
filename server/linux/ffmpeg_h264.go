package linux

import (
	"bytes"
	"fmt"
	"io"
	"log"
)

func buildH264Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "h264_nvenc" {
		surfaces := "8"
		if Chroma == "444" {
			surfaces = "16"
		}
		outputArgs = append(outputArgs, "-c:v", "h264_nvenc", "-preset", "p1", "-delay", "0", "-surfaces", surfaces, "-bf", "0", "-spatial-aq", "0", "-temporal-aq", "0", "-strict_gop", "1", "-level", "6.0", "-repeat_headers", "1", "-aud", "1")
		if NVENCLatencyMode {
			outputArgs = append(outputArgs, "-rc-lookahead", "0", "-no-scenecut", "1", "-b_ref_mode", "0")
		}
		if Chroma == "444" {
			outputArgs = append(outputArgs, "-profile:v", "high444p", "-tune", "ull", "-coder", "ac", "-pix_fmt", "bgr0", "-rgb_mode", "yuv444", "-dpb_size", "1")
		} else {
			outputArgs = append(outputArgs, "-tune", "ull")
		}
	} else {
		x264Params := fmt.Sprintf("fps=%d", fps)
		outputArgs = append(outputArgs, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", x264Params, "-level", "6.0")
		if Chroma == "444" {
			outputArgs = append(outputArgs, "-profile:v", "high444", "-pix_fmt", "yuv444p")
		}
	}
	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		multiplier := 2000
		if Chroma == "444" {
			multiplier = 4000
		}
		// Use a 2 second buffer (bw*2000) to prevent VBV underflows at high framerates with large I-frames
		bufSizeStr := fmt.Sprintf("%dk", bw*multiplier)

		if vbr {
			if VideoCodec == "h264_nvenc" {
				outputArgs = append(outputArgs,
					"-rc", "vbr",
					"-cq", "30",
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			} else {
				crf := 28 + (vbrThreshold / 50)
				if crf > 51 {
					crf = 51
				}
				outputArgs = append(outputArgs,
					"-crf", fmt.Sprintf("%d", crf),
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			}
		} else {
			if VideoCodec == "h264_nvenc" {
				outputArgs = append(outputArgs, "-b:v", bitrateStr, "-rc", "cbr")
			} else {
				outputArgs = append(outputArgs,
					"-b:v", bitrateStr,
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			}
		}
	} else {
		val := 51 - (quality-10)*33/90 // Map 10-100 to 51-18
		if vbr {
			val += (vbrThreshold / 50)
			if val > 51 {
				val = 51
			}
		}
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

func splitH264AnnexB(reader io.Reader, onFrame func(EncodedVideoFrame)) {
	buffer := make([]byte, 0, 4*1024*1024)
	temp := make([]byte, 524288)
	marker4 := []byte{0x00, 0x00, 0x00, 0x01, 0x09}
	marker3 := []byte{0x00, 0x00, 0x01, 0x09}

	nextSearchStart := 0
	for {
		n, err := reader.Read(temp)
		if n > 0 {
			oldLen := len(buffer)
			buffer = append(buffer, temp[:n]...)

			for {
				searchStart := 4
				if oldLen > 0 {
					if oldLen-4 > 4 {
						searchStart = oldLen - 4
					}
					oldLen = 0
				}
				if nextSearchStart > searchStart {
					searchStart = nextSearchStart
				}

				if len(buffer) < searchStart+5 {
					break
				}

				nextIdx := -1
				m4Idx := bytes.Index(buffer[searchStart:], marker4)
				m3Idx := bytes.Index(buffer[searchStart:], marker3)

				actualM4 := -1
				if m4Idx != -1 {
					actualM4 = m4Idx + searchStart
				}
				actualM3 := -1
				if m3Idx != -1 {
					actualM3 = m3Idx + searchStart
				}

				if actualM4 != -1 && (actualM3 == -1 || actualM4 <= actualM3) {
					nextIdx = actualM4
				} else if actualM3 != -1 {
					nextIdx = actualM3
				}

				if nextIdx != -1 {
					frame := make([]byte, nextIdx)
					copy(frame, buffer[:nextIdx])
					parsedAtMs := benchmarkClockNowMs()
					onFrame(EncodedVideoFrame{
						Data:         frame,
						ParsedAtMs:   parsedAtMs,
						LatencyTrace: startLatencyProbeEncodedFrame(parsedAtMs, 0),
					})
					buffer = buffer[nextIdx:]
					nextSearchStart = 4
				} else {
					nextSearchStart = len(buffer) - 4
					if nextSearchStart < 4 {
						nextSearchStart = 4
					}
					break
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading H264 stream: %v", err)
			}
			if len(buffer) > 0 {
				parsedAtMs := benchmarkClockNowMs()
				onFrame(EncodedVideoFrame{
					Data:         append([]byte(nil), buffer...),
					ParsedAtMs:   parsedAtMs,
					LatencyTrace: startLatencyProbeEncodedFrame(parsedAtMs, 0),
				})
			}
			return
		}
	}
}
