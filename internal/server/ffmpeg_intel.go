package server

import (
	"fmt"
)

func buildQSVH264Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	outputArgs = append(outputArgs, "-c:v", "h264_qsv", "-preset", "veryfast", "-async_depth", "1", "-bf", "0", "-aud", "1")

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			outputArgs = append(outputArgs,
				"-rc", "vbr",
				"-global_quality", "30",
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
			)
		} else {
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
				"-rc", "cbr",
			)
		}
	} else {
		val := 51 - (quality-10)*33/90
		outputArgs = append(outputArgs, "-rc", "vbr", "-global_quality", fmt.Sprintf("%d", val))

		maxKbps := 2000 + (quality-10)*18000/90
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

func buildQSVH265Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	outputArgs = append(outputArgs, "-c:v", "hevc_qsv", "-preset", "veryfast", "-async_depth", "1", "-bf", "0", "-aud", "1")

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			outputArgs = append(outputArgs,
				"-rc", "vbr",
				"-global_quality", "30",
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
			)
		} else {
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
				"-rc", "cbr",
			)
		}
	} else {
		val := 51 - (quality-10)*33/90
		outputArgs = append(outputArgs, "-rc", "vbr", "-global_quality", fmt.Sprintf("%d", val))

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

func buildQSVAV1Args(mode string, bw int, quality int, fps int, vbr bool, vbrThreshold int, keyframeInterval int) []string {
	var outputArgs []string

	outputArgs = append(outputArgs, "-c:v", "av1_qsv", "-preset", "veryfast", "-async_depth", "1", "-bf", "0")

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*2000)

		if vbr {
			outputArgs = append(outputArgs,
				"-rc", "vbr",
				"-global_quality", "35",
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
			)
		} else {
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
				"-rc", "cbr",
			)
		}
	} else {
		val := 63 - (quality-10)*50/90
		outputArgs = append(outputArgs, "-rc", "vbr", "-global_quality", fmt.Sprintf("%d", val))

		maxKbps := 2000 + (quality-10)*18000/90
		maxrateStr := fmt.Sprintf("%dk", maxKbps)
		bufsizeStr := fmt.Sprintf("%dk", maxKbps*2)
		outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
	}

	outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))
	outputArgs = append(outputArgs,
		"-max_muxing_queue_size", "1024",
		"-g", fmt.Sprintf("%d", fps*keyframeInterval),
		"-f", "ivf", "-av1_cpu_used", "8", "pipe:1",
	)

	return outputArgs
}
