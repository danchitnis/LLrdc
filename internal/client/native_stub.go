//go:build !native || !cgo || (!linux && !darwin)

package client

import "errors"

func NewNativeRenderer(_ NativeRendererOptions) (WindowRenderer, error) {
	return nil, errors.New("native renderer is unavailable in this build; build cmd/client with -tags native inside Dockerfile.client")
}
