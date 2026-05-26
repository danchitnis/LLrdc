package linux

import (
	"time"

	"github.com/danchitnis/llrdc/server/common"
)

type latencyProbeSendTrace = common.LatencyProbeSendTrace
type LatencyProbeSendTrace = common.LatencyProbeSendTrace
type inputTask = common.InputTask
type LatencyTraceRecord = common.LatencyTraceRecord

func GetScreenSize() (int, int) {
	return common.GetScreenSize()
}

func SetScreenSize(width, height int) bool {
	return common.SetScreenSize(width, height)
}

func initScreenSize(maxW, maxH int) {
	common.InitScreenSize(maxW, maxH)
}

func UpdateScreenSizeFromInitialRes() {
	common.UpdateScreenSizeFromInitialRes()
}

func GetInputChannel() chan common.InputTask {
	return common.GetInputChannel()
}

func TriggerPing() {
	common.TriggerPing()
}

func PrimeFrameGeneration(delay time.Duration, count int, interval time.Duration) {
	common.PrimeFrameGeneration(delay, count, interval)
}

func GetLinuxKeyCode(code string) int {
	return common.GetLinuxKeyCode(code)
}

func benchmarkClockNowMs() int64 {
	return common.BenchmarkClockNowMs()
}

func setLastInputReceivedAt(t int64) {
	common.SetLastInputReceivedAt(t)
}

func SnapshotLatencyTrace(markerStr string) (common.LatencyTraceRecord, bool) {
	return common.SnapshotLatencyTrace(markerStr)
}

func snapshotLatencyTrace(markerStr string) (common.LatencyTraceRecord, bool) {
	return common.SnapshotLatencyTrace(markerStr)
}

func startLatencyProbeEncodedFrame(frameAtMs int64, containerTimestamp uint64) *common.LatencyProbeSendTrace {
	return common.StartLatencyProbeEncodedFrame(frameAtMs, containerTimestamp)
}

func noteLatencyProbeFrameDispatch(trace *common.LatencyProbeSendTrace, dispatchAtMs int64) {
	common.NoteLatencyProbeFrameDispatch(trace, dispatchAtMs)
}
