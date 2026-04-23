//go:build !linux

package main

import "time"

func benchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}
