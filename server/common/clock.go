//go:build !linux

package common

import "time"

func BenchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}
