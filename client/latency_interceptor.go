package client

import (
	"strings"

	"github.com/pion/interceptor"
)

type remotePacketTimestampInterceptorFactory struct {
	now    func() int64
	record func(ssrc uint32, timestamp uint32, sequence uint16, at int64)
}

func newRemotePacketTimestampInterceptorFactory(
	now func() int64,
	record func(ssrc uint32, timestamp uint32, sequence uint16, at int64),
) interceptor.Factory {
	return &remotePacketTimestampInterceptorFactory{
		now:    now,
		record: record,
	}
}

func (f *remotePacketTimestampInterceptorFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &remotePacketTimestampInterceptor{
		NoOp:   interceptor.NoOp{},
		now:    f.now,
		record: f.record,
	}, nil
}

type remotePacketTimestampInterceptor struct {
	interceptor.NoOp
	now    func() int64
	record func(ssrc uint32, timestamp uint32, sequence uint16, at int64)
}

func (i *remotePacketTimestampInterceptor) BindRemoteStream(
	info *interceptor.StreamInfo,
	reader interceptor.RTPReader,
) interceptor.RTPReader {
	if info == nil || !strings.HasPrefix(strings.ToLower(info.MimeType), "video/") {
		return reader
	}
	ssrc := info.SSRC
	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attrs, err := reader.Read(b, a)
		if err != nil {
			return n, attrs, err
		}
		if attrs == nil {
			attrs = make(interceptor.Attributes)
		}
		header, headerErr := attrs.GetRTPHeader(b[:n])
		if headerErr == nil && i.record != nil && i.now != nil {
			i.record(ssrc, header.Timestamp, header.SequenceNumber, i.now())
		}
		return n, attrs, nil
	})
}
