package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	lastClipboardMu   sync.Mutex
	lastClipboardText string
)

// startClipboardPoller polls the remote X11 clipboard every second and
// broadcasts changes to all connected clients via clipboard_get messages.
func startClipboardPoller(display string, broadcast func(msg interface{})) {
	if !EnableClipboard {
		return
	}

	go func() {
		for {
			time.Sleep(1 * time.Second)
			cmd := exec.Command("xclip", "-selection", "clipboard", "-o")
			cmd.Env = append(os.Environ(), "DISPLAY="+display)
			out, err := cmd.Output()
			if err == nil {
				text := string(out)
				lastClipboardMu.Lock()
				changed := text != lastClipboardText
				if changed {
					lastClipboardText = text
				}
				lastClipboardMu.Unlock()
				if changed {
					broadcast(map[string]interface{}{
						"type": "clipboard_get",
						"text": text,
					})
				}
			}
		}
	}()
}

// handleClipboardSet processes a clipboard_set message from the client.
// It sets the remote X11 clipboard via xclip and optionally injects Ctrl+V
// for paste operations.
func handleClipboardSet(msg map[string]interface{}, display string) {
	if !EnableClipboard {
		return
	}

	text, ok := msg["text"].(string)
	if !ok {
		return
	}

	log.Printf(">>> [Server] Setting remote clipboard: %d chars", len(text))
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	cmd.Stdin = strings.NewReader(text)
	err := cmd.Run()
	if err != nil {
		log.Printf(">>> [Server] Error running xclip: %v", err)
	} else {
		// Update the last known clipboard so the polling goroutine
		// doesn't echo this text back as clipboard_get
		lastClipboardMu.Lock()
		lastClipboardText = text
		lastClipboardMu.Unlock()
	}

	// If this is a paste operation, inject Ctrl+V after clipboard is set
	if paste, ok := msg["paste"].(bool); ok && paste && err == nil {
		log.Printf(">>> [Server] Injecting Ctrl+V after clipboard set")
		time.Sleep(50 * time.Millisecond)
		vCmd := exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+v")
		vCmd.Env = append(os.Environ(), "DISPLAY="+display)
		if vErr := vCmd.Run(); vErr != nil {
			log.Printf(">>> [Server] Error injecting Ctrl+V: %v", vErr)
		}
	}
}
