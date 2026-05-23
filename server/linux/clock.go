//go:build !linux

package linux

import "time"

func benchmarkClockNowMs() int64 {
	return time.Now().UnixMilli()
}
