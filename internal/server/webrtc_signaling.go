package server

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

func handleWebRTCOffer(msg map[string]interface{}, requestHost string, pc **webrtc.PeerConnection, writeJSON func(interface{}) error) {
	log.Println("Received webrtc_offer")
	if sdpMap, ok := msg["sdp"].(map[string]interface{}); ok {
		b, _ := json.Marshal(sdpMap)
		var sdp webrtc.SessionDescription
		err := json.Unmarshal(b, &sdp)
		if err != nil {
			log.Printf("webrtc_offer json unmarshal error: %v", err)
			return
		}

		if *pc != nil {
			log.Println("Closing previous PeerConnection")
			(*pc).Close()
		}

		codecFamily := normalizeCodecFamily(VideoCodec)
		if !remoteOfferSupportsCodec(sdp.SDP, codecFamily) {
			fallbackCodec := fallbackCodecForRemoteOffer()
			log.Printf("Remote WebRTC offer does not support %s for requested codec %s; falling back to %s", codecFamily, VideoCodec, fallbackCodec)
			if fallbackCodec != VideoCodec {
				SetVideoCodec(fallbackCodec)
				broadcastConfig(true)
			}
			_ = writeJSON(map[string]interface{}{"type": "reconnect_hint"})
			return
		}

		log.Println("Creating new PeerConnection")
		newPC, err := createPeerConnection(requestHost)
		if err != nil {
			log.Printf("Failed to create PeerConnection: %v", err)
			return
		}
		*pc = newPC

		(*pc).OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
			log.Printf("WebRTC PeerConnection state changed: %s", s.String())
			if s == webrtc.PeerConnectionStateConnected {
				PrimeFrameGeneration(0, 5, 100*time.Millisecond)
				TriggerPing()
			}
		})

		(*pc).OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
			log.Printf("WebRTC ICE connection state changed: %s", s.String())
		})

		(*pc).OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate != nil {
				writeJSON(map[string]interface{}{
					"type":      "webrtc_ice",
					"candidate": candidate.ToJSON(),
				})
			}
		})

		(*pc).OnDataChannel(func(dc *webrtc.DataChannel) {
			if dc.Label() == "input" {
				log.Println("Input DataChannel opened")
				dc.OnMessage(func(msg webrtc.DataChannelMessage) {
					if UseDebugInput {
						log.Printf("Input DataChannel message received: %s", string(msg.Data))
					}
					var inputMsg map[string]interface{}
					if err := json.Unmarshal(msg.Data, &inputMsg); err != nil {
						return
					}
					handleInputMessage(inputMsg)
				})
			}
		})

		if err := (*pc).SetRemoteDescription(sdp); err != nil {
			log.Printf("SetRemoteDescription error: %v", err)
			return
		}

		answer, err := (*pc).CreateAnswer(nil)
		if err != nil {
			log.Printf("CreateAnswer error: %v", err)
			return
		}

		if err := (*pc).SetLocalDescription(answer); err != nil {
			log.Printf("SetLocalDescription error: %v", err)
			fallbackCodec := fallbackCodecForRemoteOffer()
			if fallbackCodec != VideoCodec {
				log.Printf("Falling back to %s after WebRTC local description failure", fallbackCodec)
				SetVideoCodec(fallbackCodec)
				broadcastConfig(true)
			}
			_ = writeJSON(map[string]interface{}{"type": "reconnect_hint"})
			return
		}

		log.Println("Sending webrtc_answer")
		writeJSON(map[string]interface{}{
			"type": "webrtc_answer",
			"sdp":  (*pc).LocalDescription(),
		})

		go func(previousStreamID uint32) {
			restarted := false
			ffmpegMutex.Lock()
			if ffmpegCmd != nil && ffmpegCmd.Process != nil {
				log.Println("New WebRTC peer connected, restarting video stream to force a fresh keyframe...")
				forceKillProcess(ffmpegCmd.Process)
				restarted = true
				restarted = true
			}
			ffmpegMutex.Unlock()

			if restarted {
				PrimeFrameGeneration(0, 5, 100*time.Millisecond)
				if err := waitForStreamReadyAfter(previousStreamID, 5*time.Second); err != nil {
					log.Printf("Stream did not become ready after WebRTC reconnect: %v", err)
					PrimeFrameGeneration(0, 10, 100*time.Millisecond)
				}
				return
			}
			PrimeFrameGeneration(0, 5, 100*time.Millisecond)
			TriggerPing()
		}(getCurrentFFmpegStreamID())
	} else {
		log.Println("webrtc_offer missing 'sdp' map")
	}
}

func remoteOfferSupportsCodec(sdp string, codecFamily string) bool {
	codecFamily = normalizeCodecFamily(codecFamily)
	lowerSDP := strings.ToLower(sdp)

	switch codecFamily {
	case "vp8":
		return strings.Contains(lowerSDP, " vp8/90000")
	case "h264":
		return strings.Contains(lowerSDP, " h264/90000")
	case "h265":
		return strings.Contains(lowerSDP, " h265/90000") || strings.Contains(lowerSDP, " hevc/90000")
	case "av1":
		return strings.Contains(lowerSDP, " av1/90000")
	default:
		return true
	}
}

func fallbackCodecForRemoteOffer() string {
	if UseIntel && QSVAvailable {
		return "h264_qsv"
	}
	if UseNVIDIA {
		return "h264_nvenc"
	}
	return "h264"
}

func handleWebRTCICE(msg map[string]interface{}, pc *webrtc.PeerConnection) {
	if candidateMap, ok := msg["candidate"].(map[string]interface{}); ok {
		if pc != nil {
			b, _ := json.Marshal(candidateMap)
			var ice webrtc.ICECandidateInit
			json.Unmarshal(b, &ice)
			if err := pc.AddICECandidate(ice); err != nil {
				log.Printf("AddICECandidate error: %v", err)
			}
		}
	}
}
