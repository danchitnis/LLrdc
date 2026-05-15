//go:build !linux && !darwin && native

package main

import (
	"fmt"
	"github.com/danchitnis/llrdc/client"
)

func createRenderer(opts client.NativeRendererOptions) (client.WindowRenderer, error) {
	return nil, fmt.Errorf("native renderer not supported on this platform")
}
