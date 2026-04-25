//go:build linux

package server

import (
	"time"

	"golang.org/x/sys/unix"
)

func benchmarkClockNowMs() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err == nil {
		return ts.Nano() / int64(time.Millisecond)
	}
	return time.Now().UnixMilli()
}
