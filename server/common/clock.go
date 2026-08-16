//go:build !linux

package common

import "time"

func BenchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}

func BenchmarkClockNowNs() int64 {
	return time.Now().UnixNano()
}
