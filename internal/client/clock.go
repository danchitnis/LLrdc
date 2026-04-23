//go:build !linux

package client

import "time"

func benchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}
