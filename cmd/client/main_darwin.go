//go:build darwin && native

package main

import (
	"github.com/danchitnis/llrdc/client"
	"github.com/danchitnis/llrdc/client/macos"
)

func createRenderer(opts client.NativeRendererOptions) (client.WindowRenderer, error) {
	return macos.NewNativeRenderer(opts)
}
