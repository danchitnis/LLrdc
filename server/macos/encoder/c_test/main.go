package main

import (
	"fmt"
	"github.com/danchitnis/llrdc/server/macos/encoder"
)

func main() {
	width, height := 1920, 1080
	enc := encoder.NewVTEncoder("h264", width, height, 60, 5000, 0, func(data []byte, keyframe bool) {
		fmt.Printf("Got frame! length: %d\n", len(data))
	})

	// Fake YUV data
	buf := make([]byte, width*height*3/2)
	enc.Encode(buf)

	// Sleep to allow async callback
	select {}
}
