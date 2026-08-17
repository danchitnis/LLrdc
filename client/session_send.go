package client

import (
	"encoding/json"
	"errors"
	"time"
)

func (s *Session) SendResize(width, height int) error {
	s.mu.Lock()
	s.state.LastResizeWidth = width
	s.state.LastResizeHeight = height
	s.state.LastResizeAt = time.Now()
	s.mu.Unlock()
	return s.sendJSON(map[string]any{
		"type":   "resize",
		"width":  width,
		"height": height,
	})
}

func (s *Session) SendConfig(config map[string]any) error {
	msg := cloneMap(config)
	msg["type"] = "config"

	// Upgrade codec if it's a generic one and hardware is available
	s.mu.RLock()
	lastConfig := s.state.LastConfig
	s.mu.RUnlock()

	if vCodec, ok := msg["videoCodec"].(string); ok && lastConfig != nil {
		if vCodec == "h264" {
			if vaapi, _ := lastConfig["vaapiAvailable"].(bool); vaapi {
				msg["videoCodec"] = "h264_vaapi"
			} else if nvenc, _ := lastConfig["nvidiaAvailable"].(bool); nvenc {
				msg["videoCodec"] = "h264_nvenc"
			}
		} else if vCodec == "h265" {
			hasMacOSEncoder := false
			if caps, ok := lastConfig["capabilities"].(map[string]any); ok {
				if combos, ok := caps["valid_combinations"].([]any); ok {
					for _, combo := range combos {
						if comboMap, ok := combo.(map[string]any); ok {
							encoder, _ := comboMap["encoder"].(string)
							codec, _ := comboMap["codec"].(string)
							if (codec == "h265" || codec == "hevc") && encoder == "macos" {
								hasMacOSEncoder = true
								break
							}
						}
					}
				}
			}

			if vt, _ := lastConfig["vtAvailable"].(bool); vt || hasMacOSEncoder {
				msg["videoCodec"] = "h265"
			} else if vaapi, _ := lastConfig["h265VaapiAvailable"].(bool); vaapi {
				msg["videoCodec"] = "h265_vaapi"
			} else if nvenc, _ := lastConfig["h265Nvenc444Available"].(bool); nvenc {
				msg["videoCodec"] = "h265_nvenc"
			} else if intel, _ := lastConfig["intelAvailable"].(bool); intel {
				msg["videoCodec"] = "hevc_vaapi"
			} else {
				msg["videoCodec"] = "h265"
			}
		} else if vCodec == "av1" {
			if vaapi, _ := lastConfig["av1VaapiAvailable"].(bool); vaapi {
				msg["videoCodec"] = "av1_vaapi"
			} else if nvenc, _ := lastConfig["av1NvencAvailable"].(bool); nvenc {
				msg["videoCodec"] = "av1_nvenc"
			}
		}
	}

	return s.sendJSON(msg)
}

func (s *Session) SendInput(msg map[string]any) error {
	payload := cloneMap(msg)
	if sampleID, ok := numberToInt64(payload["sampleId"]); ok && sampleID > 0 {
		payload["clientInputSendNs"] = BenchmarkClockNowNs()
	}
	s.emit(EventInputSent, cloneMap(payload))
	return s.sendMessage(payload)
}

func (s *Session) SendPing() error {
	return s.sendMessage(map[string]any{
		"type": "ping",
		"ts":   BenchmarkClockNowMs(),
	})
}

func (s *Session) sendMessage(msg map[string]any) error {
	s.mu.Lock()
	payload := cloneMap(msg)
	if sampleID, ok := numberToInt64(payload["sampleId"]); ok && sampleID > 0 {
		// Stamp immediately before serializing the control message so the
		// server-side value represents the native send boundary, not event setup.
		payload["clientInputSendNs"] = BenchmarkClockNowNs()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if msgType, _ := msg["type"].(string); msgType == "mousemove" || msgType == "mousebtn" || msgType == "keydown" || msgType == "keyup" || msgType == "wheel" {
		s.stats.InputMessagesSent++
	}
	conn := s.conn
	wtControl := s.wtControl
	s.mu.Unlock()

	if wtControl != nil {
		_, err := wtControl.Write(append(body, '\n'))
		return err
	}

	if conn == nil {
		return errors.New("session is not connected")
	}
	return s.sendRaw(body)
}
