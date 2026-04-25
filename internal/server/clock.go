//go:build !linux

package server

import "time"

func benchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}
