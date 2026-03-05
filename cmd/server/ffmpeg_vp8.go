package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
)

func buildVP8Args(mode string, bw int, quality int, fps int, cpuEffort int, cpuThreads int, vbr bool) []string {
	var outputArgs []string

	outputArgs = append(outputArgs, "-c:v", "libvpx")

	if mode == "bandwidth" {
		bitrateStr := fmt.Sprintf("%dk", bw*1000)
		bufSizeStr := fmt.Sprintf("%dk", bw*200)

		outputArgs = append(outputArgs,
			"-b:v", bitrateStr,
			"-maxrate", bitrateStr,
			"-bufsize", bufSizeStr,
			"-crf", "20", 
			"-static-thresh", "1000",
		)
	} else {
		crf := 50 - (quality-10)*46/90
		if crf < 4 {
			crf = 4
		}
		if crf > 63 {
			crf = 63
		}
		outputArgs = append(outputArgs, "-crf", fmt.Sprintf("%d", crf), "-qmin", fmt.Sprintf("%d", crf))
		maxKbps := 2000 + (quality-10)*18000/90
		maxrateStr := fmt.Sprintf("%dk", maxKbps)
		bufsizeStr := fmt.Sprintf("%dk", maxKbps/5)
		outputArgs = append(outputArgs, "-maxrate", maxrateStr, "-bufsize", bufsizeStr)
	}

	cpuUsedStr := fmt.Sprintf("%d", cpuEffort)

	if !vbr {
		outputArgs = append(outputArgs, "-r", fmt.Sprintf("%d", fps))
	}

	outputArgs = append(outputArgs,
		"-lag-in-frames", "0",
		"-error-resilient", "1",
		"-rc_lookahead", "0",
		"-g", fmt.Sprintf("%d", fps),
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
	headerData := make([]byte, 32)
	if _, err := io.ReadFull(reader, headerData); err != nil {
		log.Printf("Failed to read IVF header: %v", err)
		return
	}
	if string(headerData[:4]) != "DKIF" {
		log.Printf("Invalid IVF signature: %s", string(headerData[:4]))
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
