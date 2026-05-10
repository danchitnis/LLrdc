package splitproto

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHeaderEncodeDecode(t *testing.T) {
	h := &Header{
		Magic:      [4]byte{'L', 'L', 'S', 'P'},
		Version:    1,
		Generation: 123,
		Width:      1920,
		Height:     1080,
		FPS:        60,
		PixFmt:     1,
	}

	buf := h.Encode()
	h2, err := DecodeHeader(buf)
	if err != nil {
		t.Fatalf("DecodeHeader failed: %v", err)
	}

	if !reflect.DeepEqual(h, h2) {
		t.Errorf("Headers not equal: %+v != %+v", h, h2)
	}
}

func TestMessageJSON(t *testing.T) {
	m := Message{
		Type: MsgApplyConfig,
		Config: map[string]interface{}{
			"width":      float64(1280),
			"height":     float64(720),
			"generation": float64(456),
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var m2 Message
	if err := json.Unmarshal(data, &m2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if m.Type != m2.Type {
		t.Errorf("Types not equal: %s != %s", m.Type, m2.Type)
	}

	if m.Config["width"].(float64) != m2.Config["width"].(float64) {
		t.Errorf("Widths not equal")
	}
}
