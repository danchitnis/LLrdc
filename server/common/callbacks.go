package common

import (
	"log"
)

var (
	// OnForceKeyframe is triggered when a keyframe is requested (e.g., via signaling or RTCP PLI).
	OnForceKeyframe func()

	// OnPeerConnected is triggered when a new WebRTC peer is fully connected.
	OnPeerConnected func()

	// OnTriggerPing is triggered to inject a damage-tracking ping into the compositor.
	OnTriggerPing func()

	// OnInputMessage is triggered when a new input control event arrives on the WebRTC data channel.
	OnInputMessage func(msg map[string]interface{})

	// OnConfigChanged is triggered when a fallback or runtime configuration change is applied.
	OnConfigChanged func()

	// OnFallbackCodec is triggered when a dynamic codec fallback is needed.
	OnFallbackCodec func(codec string)
)

// SafeForceKeyframe runs OnForceKeyframe safely.
func SafeForceKeyframe() {
	if OnForceKeyframe != nil {
		OnForceKeyframe()
	} else {
		log.Println("OnForceKeyframe callback is not registered")
	}
}

// SafePeerConnected runs OnPeerConnected safely.
func SafePeerConnected() {
	if OnPeerConnected != nil {
		OnPeerConnected()
	}
}

// SafeTriggerPing runs OnTriggerPing safely.
func SafeTriggerPing() {
	if OnTriggerPing != nil {
		OnTriggerPing()
	}
}

// SafeInputMessage runs OnInputMessage safely.
func SafeInputMessage(msg map[string]interface{}) {
	if OnInputMessage != nil {
		OnInputMessage(msg)
	}
}

// SafeConfigChanged runs OnConfigChanged safely.
func SafeConfigChanged() {
	if OnConfigChanged != nil {
		OnConfigChanged()
	}
}

// SafeFallbackCodec runs OnFallbackCodec safely.
func SafeFallbackCodec(codec string) {
	if OnFallbackCodec != nil {
		OnFallbackCodec(codec)
	}
}

