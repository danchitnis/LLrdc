//go:build linux

package client

import (
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// Use the monotonic clock advertised by Wayland presentation feedback so all
// benchmark participants report timestamps in the same clock domain.
func benchmarkClockID() int32 {
	if id, err := strconv.Atoi(os.Getenv("LLRDC_PRESENTATION_CLOCK_ID")); err == nil {
		switch id {
		case 4:
			return unix.CLOCK_MONOTONIC_RAW
		case 1:
			return unix.CLOCK_MONOTONIC
		}
	}
	return unix.CLOCK_MONOTONIC
}

func BenchmarkClockNowMs() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(benchmarkClockID(), &ts); err == nil {
		return ts.Nano() / int64(time.Millisecond)
	}
	return time.Now().UnixMilli()
}

func BenchmarkClockNowNs() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(benchmarkClockID(), &ts); err == nil {
		return ts.Nano()
	}
	return time.Now().UnixNano()
}
