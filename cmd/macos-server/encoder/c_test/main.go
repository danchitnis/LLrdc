package main

import (
	"fmt"
	"github.com/danchitnis/llrdc/cmd/macos-server/encoder"
)

func main() {
	width, height := 1920, 1080
	enc := encoder.NewVTEncoder(width, height, 60, 5000, func(data []byte, keyframe bool) {
		fmt.Printf("Got frame! length: %d\n", len(data))
	})

	// Fake YUV data
	buf := make([]byte, width*height*3/2)
	enc.Encode(buf)

	// Sleep to allow async callback
	select {}
}
