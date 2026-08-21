package common

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// OnForceKeyframe is triggered when a client requests a keyframe.
	OnForceKeyframe func()

	// OnClientConnected is triggered when a new client is fully connected.
	OnClientConnected func()

	// OnClientMediaConnected is triggered after the media stream is ready.
	OnClientMediaConnected func()

	// OnClientDisconnected is triggered when a client disconnects.
	OnClientDisconnected func()

	// OnPauseStreaming is triggered when client-timeout expires and streaming should pause.
	OnPauseStreaming func()

	// OnResumeStreaming is triggered when a client reconnects and streaming should resume.
	OnResumeStreaming func()

	// OnTriggerPing is triggered to inject a damage-tracking ping into the compositor.
	OnTriggerPing func()

	// OnInputMessage is triggered when a new input control event arrives.
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

// SafeClientConnected runs OnClientConnected safely.
func SafeClientConnected() {
	if OnClientConnected != nil {
		OnClientConnected()
	}
}

// SafeClientMediaConnected runs OnClientMediaConnected safely.
func SafeClientMediaConnected() {
	if OnClientMediaConnected != nil {
		OnClientMediaConnected()
	}
}

// SafeClientDisconnected runs OnClientDisconnected safely.
func SafeClientDisconnected() {
	if OnClientDisconnected != nil {
		OnClientDisconnected()
	}
}

// SafePauseStreaming runs OnPauseStreaming safely.
func SafePauseStreaming() {
	if OnPauseStreaming != nil {
		OnPauseStreaming()
	}
}

// SafeResumeStreaming runs OnResumeStreaming safely.
func SafeResumeStreaming() {
	if OnResumeStreaming != nil {
		OnResumeStreaming()
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

var (
	clientTimeoutTimer   *time.Timer
	clientTimeoutMutex   sync.Mutex
	streamingIsPaused    bool
	expectingReconnect   bool
	expectingReconnectMu sync.Mutex
	ActiveClientCount    int64
)

func SetExpectingReconnect(value bool) {
	expectingReconnectMu.Lock()
	expectingReconnect = value
	expectingReconnectMu.Unlock()
}

func isExpectingReconnect() bool {
	expectingReconnectMu.Lock()
	defer expectingReconnectMu.Unlock()
	return expectingReconnect
}

func GetConnectedClientCount() int {
	return int(atomic.LoadInt64(&ActiveClientCount))
}

func HandleClientConnectionChange() {
	if ClientTimeout <= 0 {
		return
	}

	clientTimeoutMutex.Lock()
	defer clientTimeoutMutex.Unlock()

	count := GetConnectedClientCount()
	if count > 0 {
		if clientTimeoutTimer != nil {
			clientTimeoutTimer.Stop()
			clientTimeoutTimer = nil
		}
		if streamingIsPaused {
			streamingIsPaused = false
			log.Println("Client connected. Resuming streaming.")
			SafeResumeStreaming()
		}
		SetExpectingReconnect(false)
	} else {
		if isExpectingReconnect() {
			log.Println("Expecting reconnect due to config/display change; skipping inactivity timer.")
			if clientTimeoutTimer != nil {
				clientTimeoutTimer.Stop()
				clientTimeoutTimer = nil
			}
			return
		}

		if clientTimeoutTimer == nil && !streamingIsPaused {
			log.Printf("All clients disconnected. Starting inactivity timer: %d seconds", ClientTimeout)
			clientTimeoutTimer = time.AfterFunc(time.Duration(ClientTimeout)*time.Second, func() {
				clientTimeoutMutex.Lock()
				defer clientTimeoutMutex.Unlock()
				if GetConnectedClientCount() == 0 && !streamingIsPaused && !isExpectingReconnect() {
					streamingIsPaused = true
					log.Printf("No clients connected for %d seconds. Pausing streaming to save CPU/GPU resources.", ClientTimeout)
					SafePauseStreaming()
				}
				clientTimeoutTimer = nil
			})
		}
	}
}

func StartClientTimeoutTracker() {
	HandleClientConnectionChange()
}
