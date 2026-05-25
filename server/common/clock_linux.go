//go:build linux

package common

import (
	"time"

	"golang.org/x/sys/unix"
)

func BenchmarkClockNowMs() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err == nil {
		return ts.Nano() / int64(time.Millisecond)
	}
	return time.Now().UnixMilli()
}
