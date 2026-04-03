package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
)

func buildVP8Args(mode string, bw int, quality int, fps int, cpuEffort int, cpuThreads int, vbr bool, keyframeInterval int) []string {
	var outputArgs []string

	outputArgs = append(outputArgs, "-c:v", "libvpx")

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*200)

		if vbr {
			// libvpx VBR: set a target bitrate but allow it to drop to 0 if static
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-bufsize", bufSizeStr,
				"-crf", "20",
				"-static-thresh", "1000",
			)
		} else {
			// CBR
			outputArgs = append(outputArgs,
				"-b:v", bitrateStr,
				"-maxrate", bitrateStr,
				"-minrate", bitrateStr,
				"-bufsize", bufSizeStr,
				"-static-thresh", "0",
			)
		}
	} else {
		crf := 50 - (quality-10)*46/90
		if crf < 4 {
			crf = 4
		}
		// For VP8 quality mode, use a nominal bitrate + CRF
		outputArgs = append(outputArgs,
			"-b:v", "2M",
			"-crf", fmt.Sprintf("%d", crf),
			"-static-thresh", "1000",
		)

		maxKbps := 2000 + (quality-10)*18000/90
		maxrateStr := fmt.Sprintf("%dk", maxKbps)
		bufsizeStr := fmt.Sprintf("%dk", maxKbps/5)
		outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
	}

	cpuUsedStr := fmt.Sprintf("%d", cpuEffort)

	outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))

	outputArgs = append(outputArgs,
		"-lag-in-frames", "0",
		"-error-resilient", "1",
		"-rc_lookahead", "0",
		"-g", fmt.Sprintf("%d", fps*keyframeInterval),
		"-deadline", "realtime",
		"-cpu-used", cpuUsedStr,
		"-threads", fmt.Sprintf("%d", cpuThreads),
		"-speed", "8",
		"-flush_packets", "1",
		"-f", "ivf",
		"pipe:1",
	)

	return outputArgs
}

func splitIVF(reader io.Reader, onFrame func([]byte)) {
	// Scan for "DKIF"
	headerData := make([]byte, 32)
	found := false
	for {
		b := make([]byte, 1)
		if _, err := io.ReadFull(reader, b); err != nil {
			log.Printf("Failed to read while scanning for IVF header: %v", err)
			return
		}
		if b[0] == 'D' {
			headerData[0] = 'D'
			if _, err := io.ReadFull(reader, headerData[1:32]); err != nil {
				log.Printf("Failed to read IVF header: %v", err)
				return
			}
			if string(headerData[0:4]) == "DKIF" {
				found = true
				break
			}
		}
	}

	if !found {
		log.Printf("Could not find IVF signature 'DKIF'")
		return
	}

	for {
		frameHeader := make([]byte, 12)
		if _, err := io.ReadFull(reader, frameHeader); err != nil {
			if err != io.EOF {
				log.Printf("Error reading frame header: %v", err)
			}
			return
		}

		frameSize := binary.LittleEndian.Uint32(frameHeader[0:4])
		frameData := make([]byte, frameSize)
		if _, err := io.ReadFull(reader, frameData); err != nil {
			log.Printf("Error reading frame data: %v", err)
			return
		}

		onFrame(frameData)
	}
}
