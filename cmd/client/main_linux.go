//go:build linux && native

package main

import (
	"github.com/danchitnis/llrdc/client"
	"github.com/danchitnis/llrdc/client/wayland"
)

func createRenderer(opts client.NativeRendererOptions) (client.WindowRenderer, error) {
	return wayland.NewNativeRenderer(opts)
}
