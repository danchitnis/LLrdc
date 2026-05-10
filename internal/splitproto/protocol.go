package splitproto

import (
	"encoding/binary"
	"errors"
)

const (
	ControlPort = 12348
	VideoPort   = 12345
	InputPort   = 12346
)

const Magic = "LLSP"
const Version = 1

// Header size is 24 bytes
const HeaderSize = 24

type Header struct {
	Magic      [4]byte
	Version    uint32
	Generation uint64
	Width      uint16
	Height     uint16
	FPS        uint16
	PixFmt     uint16 // 0 for YUV420p, 1 for YUV444p
}

func (h *Header) Encode() []byte {
	buf := make([]byte, HeaderSize)
	copy(buf[0:4], h.Magic[:])
	binary.BigEndian.PutUint32(buf[4:8], h.Version)
	binary.BigEndian.PutUint64(buf[8:16], h.Generation)
	binary.BigEndian.PutUint16(buf[16:18], h.Width)
	binary.BigEndian.PutUint16(buf[18:20], h.Height)
	binary.BigEndian.PutUint16(buf[20:22], h.FPS)
	binary.BigEndian.PutUint16(buf[22:24], h.PixFmt)
	return buf
}

func DecodeHeader(buf []byte) (*Header, error) {
	if len(buf) < HeaderSize {
		return nil, errors.New("header too short")
	}
	h := &Header{}
	copy(h.Magic[:], buf[0:4])
	if string(h.Magic[:]) != Magic {
		return nil, errors.New("invalid magic")
	}
	h.Version = binary.BigEndian.Uint32(buf[4:8])
	h.Generation = binary.BigEndian.Uint64(buf[8:16])
	h.Width = binary.BigEndian.Uint16(buf[16:18])
	h.Height = binary.BigEndian.Uint16(buf[18:20])
	h.FPS = binary.BigEndian.Uint16(buf[20:22])
	h.PixFmt = binary.BigEndian.Uint16(buf[22:24])
	return h, nil
}

type Message struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

const (
	MsgApplyConfig   = "apply_config"
	MsgConfigApplied = "config_applied"
	MsgStreamStarted = "stream_started"
	MsgFirstFrame    = "first_frame"
	MsgForceKeyframe = "force_keyframe"
	MsgError         = "error"
)
