package server

import "testing"

func TestRemoteOfferSupportsCodec(t *testing.T) {
	sdp := "v=0\r\na=rtpmap:96 VP8/90000\r\na=rtpmap:102 H264/90000\r\n"

	if !remoteOfferSupportsCodec(sdp, "h264_qsv") {
		t.Fatal("expected H.264 QSV to be supported by H.264 rtpmap")
	}
	if remoteOfferSupportsCodec(sdp, "av1_qsv") {
		t.Fatal("expected AV1 QSV to be unsupported without AV1 rtpmap")
	}
}

func TestFallbackCodecForRemoteOfferPrefersIntelH264(t *testing.T) {
	oldUseIntel := UseIntel
	oldQSVAvailable := QSVAvailable
	oldUseNVIDIA := UseNVIDIA
	defer func() {
		UseIntel = oldUseIntel
		QSVAvailable = oldQSVAvailable
		UseNVIDIA = oldUseNVIDIA
	}()

	UseIntel = true
	QSVAvailable = true
	UseNVIDIA = false

	if got, want := fallbackCodecForRemoteOffer(), "h264_qsv"; got != want {
		t.Fatalf("unexpected fallback codec: got %s want %s", got, want)
	}
}
