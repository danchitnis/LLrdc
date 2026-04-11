package main

import (
	"fmt"
	"io"
	"log"
)

func buildH265Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "h265_nvenc" {
		outputArgs = append(outputArgs, "-c:v", "hevc_nvenc", "-preset", "p1", "-tune", "ull", "-delay", "0", "-surfaces", "64", "-bf", "0", "-spatial-aq", "0", "-temporal-aq", "0", "-strict_gop", "1", "-aud", "1")
		if NVENCLatencyMode {
			outputArgs = append(outputArgs, "-rc-lookahead", "0", "-no-scenecut", "1", "-b_ref_mode", "0")
		}
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
		if vbr {
			val += (vbrThreshold / 50)
			if val > 51 {
				val = 51
			}
		}
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

	outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))

	outputArgs = append(outputArgs,
		"-max_muxing_queue_size", "1024",
		"-g", fmt.Sprintf("%d", fps*keyframeInterval),
		"-f", "hevc", "pipe:1",
	)

	return outputArgs
}

func findAnnexBStartCode(data []byte, from int) (int, int, bool) {
	if from < 0 {
		from = 0
	}
	for i := from; i+3 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		if data[i+2] == 1 {
			return i, 3, true
		}
		if i+4 < len(data) && data[i+2] == 0 && data[i+3] == 1 {
			return i, 4, true
		}
	}
	return -1, 0, false
}

func h265NALType(nal []byte) (int, int, bool) {
	start, prefixLen, ok := findAnnexBStartCode(nal, 0)
	if !ok || start+prefixLen+1 >= len(nal) {
		return -1, 0, false
	}
	return int((nal[start+prefixLen] & 0x7e) >> 1), prefixLen, true
}

func isH265VCLNAL(nalType int) bool {
	return nalType >= 0 && nalType <= 31
}

func isH265PrefixBoundaryNAL(nalType int) bool {
	switch nalType {
	case 32, 33, 34, 35, 36, 37, 38, 39:
		return true
	default:
		return false
	}
}

func isH265SuffixNAL(nalType int) bool {
	return nalType == 40
}

func isH265FirstSliceSegment(nal []byte, prefixLen int) bool {
	payloadIdx := prefixLen + 2
	if payloadIdx >= len(nal) {
		return false
	}
	return nal[payloadIdx]&0x80 != 0
}

func joinNALUnits(nals [][]byte) []byte {
	totalLen := 0
	for _, nal := range nals {
		totalLen += len(nal)
	}

	frame := make([]byte, 0, totalLen)
	for _, nal := range nals {
		frame = append(frame, nal...)
	}
	return frame
}

func splitH265AnnexB(reader io.Reader, onFrame func([]byte)) {
	buffer := make([]byte, 0, 1024*1024)
	temp := make([]byte, 16384)
	pendingPrefix := make([][]byte, 0, 4)
	currentAU := make([][]byte, 0, 8)
	currentHasVCL := false

	emitCurrent := func() {
		if len(currentAU) == 0 {
			return
		}
		onFrame(joinNALUnits(currentAU))
		currentAU = currentAU[:0]
		currentHasVCL = false
	}

	processNAL := func(nal []byte) {
		if len(nal) == 0 {
			return
		}

		nalCopy := append([]byte(nil), nal...)
		nalType, prefixLen, ok := h265NALType(nalCopy)
		if !ok {
			if currentHasVCL {
				currentAU = append(currentAU, nalCopy)
			} else {
				pendingPrefix = append(pendingPrefix, nalCopy)
			}
			return
		}

		if isH265VCLNAL(nalType) {
			if isH265FirstSliceSegment(nalCopy, prefixLen) && currentHasVCL {
				emitCurrent()
			}
			if len(currentAU) == 0 && len(pendingPrefix) > 0 {
				currentAU = append(currentAU, pendingPrefix...)
				pendingPrefix = pendingPrefix[:0]
			}
			currentAU = append(currentAU, nalCopy)
			currentHasVCL = true
			return
		}

		if isH265SuffixNAL(nalType) && currentHasVCL {
			currentAU = append(currentAU, nalCopy)
			return
		}

		if currentHasVCL && isH265PrefixBoundaryNAL(nalType) {
			emitCurrent()
		}

		pendingPrefix = append(pendingPrefix, nalCopy)
	}

	for {
		n, err := reader.Read(temp)
		if n > 0 {
			buffer = append(buffer, temp[:n]...)
			for {
				startIdx, prefixLen, ok := findAnnexBStartCode(buffer, 0)
				if !ok {
					if len(buffer) > 4 {
						buffer = append([]byte(nil), buffer[len(buffer)-4:]...)
					}
					break
				}
				if startIdx > 0 {
					buffer = buffer[startIdx:]
				}

				nextIdx, _, hasNext := findAnnexBStartCode(buffer, prefixLen)
				if !hasNext {
					break
				}

				processNAL(buffer[:nextIdx])
				buffer = buffer[nextIdx:]
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading H265 stream: %v", err)
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
