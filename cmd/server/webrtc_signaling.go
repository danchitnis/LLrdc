package main

import (
	"encoding/json"
	"log"

	"github.com/pion/webrtc/v4"
)

func handleWebRTCOffer(msg map[string]interface{}, pc **webrtc.PeerConnection, writeJSON func(interface{}) error) {
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
			(*pc).Close()
		}

		newPC, err := createPeerConnection()
		if err != nil {
			log.Printf("Failed to create PeerConnection: %v", err)
			return
		}
		*pc = newPC

		(*pc).OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate != nil {
				cJSON := candidate.ToJSON()
				writeJSON(map[string]interface{}{
					"type":      "webrtc_ice",
					"candidate": cJSON,
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
			return
		}

		log.Println("Sending webrtc_answer")
		writeJSON(map[string]interface{}{
			"type": "webrtc_answer",
			"sdp":  (*pc).LocalDescription(),
		})
	} else {
		log.Println("webrtc_offer missing 'sdp' map")
	}
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
