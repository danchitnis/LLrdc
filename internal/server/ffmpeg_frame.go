package server

type EncodedVideoFrame struct {
	Data               []byte
	ParsedAtMs         int64
	ContainerTimestamp uint64
	LatencyTrace       *latencyProbeSendTrace
}
