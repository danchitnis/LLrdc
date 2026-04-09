package main

import (
	"fmt"
)

func buildAV1Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	if VideoCodec == "av1_nvenc" {
		outputArgs = append(outputArgs, "-c:v", "av1_nvenc", "-preset", "p1", "-tune", "ull", "-delay", "0", "-surfaces", "64", "-bf", "0", "-spatial-aq", "0", "-temporal-aq", "0", "-strict_gop", "1")
		if NVENCLatencyMode {
			outputArgs = append(outputArgs, "-rc-lookahead", "0", "-no-scenecut", "1", "-b_ref_mode", "0")
		}
		// Note: AV1 NVENC does NOT support 4:4:4 chroma (NVENC SDK limitation).
		// Unlike H.264 NVENC (high444p profile), there is no 444 profile for AV1 NVENC.
		// The server probe in config.go correctly detects this and disables the option.
	} else {
		// libaom-av1 is slow, but we provide it as a software fallback
		outputArgs = append(outputArgs, "-c:v", "libaom-av1", "-cpu-used", "8", "-usage", "realtime", "-row-mt", "1", "-lag-in-frames", "0", "-error-resilient", "1", "-static-thresh", "0")
	}

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			if VideoCodec == "av1_nvenc" {
				outputArgs = append(outputArgs,
					"-rc", "vbr",
					"-cq", "35",
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
				)
			} else {
				outputArgs = append(outputArgs,
					"-crf", "35",
					"-b:v", bitrateStr,
					"-maxrate", bitrateStr,
					"-bufsize", bufSizeStr,
					"-static-thresh", fmt.Sprintf("%d", vbrThreshold),
				)
			}
		} else {
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
			)
			if VideoCodec == "av1_nvenc" {
				outputArgs = append(outputArgs, "-rc", "cbr")
			}
		}
	} else {
		// Quality mode
		val := 63 - (quality-10)*50/90 // Map 10-100 to 63-13 (CRF/CQ range)
		if vbr {
			val += (vbrThreshold / 20)
			if val > 63 { val = 63 }
		}
		if VideoCodec == "av1_nvenc" {
			outputArgs = append(outputArgs, "-rc", "vbr", "-cq", fmt.Sprintf("%d", val))
		} else {
			outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", val), "-static-thresh", fmt.Sprintf("%d", vbrThreshold))
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
		"-f", "ivf",
		"pipe:1",
	)

	return outputArgs
}
