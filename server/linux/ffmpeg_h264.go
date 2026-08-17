package linux

import (
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
	currentAU := make([][]byte, 0, 8)
	currentHasVCL := false
	// Keep the latest parameter sets available. Some NVENC forced-IDR
	// outputs contain only the IDR slice even though the encoder was asked to
	// repeat SPS/PPS; every emitted VCL access unit must remain independently
	// decodable for a reconnecting native client.
	parameterSets := make(map[int][]byte, 2)

	emitCurrent := func() {
		if !currentHasVCL && len(currentAU) == 0 {
			return
		}

		parsedAtMs := benchmarkClockNowMs()

		if len(currentAU) > 0 {
			outputAU := currentAU
			if currentHasVCL && len(parameterSets) > 0 {
				hasSPS, hasPPS := false, false
				for _, nal := range currentAU {
					switch h264NALType(nal) {
					case 7:
						hasSPS = true
					case 8:
						hasPPS = true
					}
				}
				prefix := make([][]byte, 0, 2)
				if !hasSPS {
					if sps := parameterSets[7]; len(sps) > 0 {
						prefix = append(prefix, sps)
					}
				}
				if !hasPPS {
					if pps := parameterSets[8]; len(pps) > 0 {
						prefix = append(prefix, pps)
					}
				}
				if len(prefix) > 0 {
					outputAU = append(prefix, currentAU...)
				}
			}
			onFrame(EncodedVideoFrame{
				Data:         joinNALUnits(outputAU),
				ParsedAtMs:   parsedAtMs,
				LatencyTrace: startLatencyProbeEncodedFrame(parsedAtMs, 0),
			})
		}

		currentAU = currentAU[:0]
		currentHasVCL = false
	}

	processNAL := func(nal []byte) {
		if len(nal) == 0 {
			return
		}

		nalCopy := append([]byte(nil), nal...)
		start, prefixLen, ok := findAnnexBStartCode(nalCopy, 0)
		if !ok || start+prefixLen >= len(nalCopy) {
			currentAU = append(currentAU, nalCopy)
			return
		}

		headerByte := nalCopy[start+prefixLen]
		nalType := int(headerByte & 0x1f)
		if nalType == 7 || nalType == 8 {
			parameterSets[nalType] = nalCopy
		}

		isVCL := nalType == 1 || nalType == 5

		if isVCL {
			payloadIdx := start + prefixLen + 1
			isFirstSlice := true
			if payloadIdx < len(nalCopy) {
				isFirstSlice = (nalCopy[payloadIdx] & 0x80) != 0
			}

			if isFirstSlice && currentHasVCL {
				emitCurrent()
			}
			currentAU = append(currentAU, nalCopy)
			currentHasVCL = true
		} else {
			if currentHasVCL && (nalType == 9 || nalType == 7 || nalType == 8) {
				emitCurrent()
			}
			currentAU = append(currentAU, nalCopy)
		}
	}

	nextSearchStart := 0
	for {
		n, err := reader.Read(temp)
		if n > 0 {
			oldLen := len(buffer)
			buffer = append(buffer, temp[:n]...)

			for {
				startIdx, prefixLen, ok := findAnnexBStartCode(buffer, 0)
				if !ok {
					if len(buffer) > 4 {
						buffer = append([]byte(nil), buffer[len(buffer)-4:]...)
					}
					nextSearchStart = 0
					break
				}
				if startIdx > 0 {
					buffer = buffer[startIdx:]
					nextSearchStart = 0
					continue
				}

				searchStart := prefixLen
				if oldLen > 0 {
					if oldLen-4 > prefixLen {
						searchStart = oldLen - 4
					}
					oldLen = 0
				}
				if nextSearchStart > searchStart {
					searchStart = nextSearchStart
				}

				nextIdx, nextPrefixLen, hasNext := findAnnexBStartCode(buffer, searchStart)
				if !hasNext {
					nextSearchStart = len(buffer) - 4
					if nextSearchStart < prefixLen {
						nextSearchStart = prefixLen
					}
					break
				}

				processNAL(buffer[:nextIdx])
				buffer = buffer[nextIdx:]
				nextSearchStart = nextPrefixLen
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading H264 stream: %v", err)
			}
			if len(buffer) > 0 {
				if startIdx, _, ok := findAnnexBStartCode(buffer, 0); ok {
					processNAL(buffer[startIdx:])
				}
			}
			emitCurrent()
			return
		}
	}
}

func h264NALType(nal []byte) int {
	start, prefixLen, ok := findAnnexBStartCode(nal, 0)
	if !ok || start+prefixLen >= len(nal) {
		return -1
	}
	return int(nal[start+prefixLen] & 0x1f)
}
