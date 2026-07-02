package linux

import (
	"bufio"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/danchitnis/llrdc/internal/splitproto"
)

var (
	lastClipboardMu   sync.Mutex
	lastClipboardText string
)

// StartClipboardPoller starts the instant Wayland clipboard listener and
// broadcasts changes to all connected clients via clipboard_get messages.
func StartClipboardPoller() {
	if !EnableClipboard {
		return
	}

	log.Println("Starting instant Wayland clipboard listener via wl-paste watch...")
	go func() {
		// Start wl-paste watch with sh -c cat + custom delimiter
		cmd := exec.Command("wl-paste", "--watch", "sh", "-c", `cat; printf "\n---LLRDC_CLIPBOARD_END---\n"`)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Failed to create stdout pipe for wl-paste watch: %v", err)
			return
		}
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start wl-paste watch: %v", err)
			return
		}
		defer cmd.Process.Kill()

		scanner := bufio.NewScanner(stdout)
		var buf strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "---LLRDC_CLIPBOARD_END---" {
				text := buf.String()
				// Remove the trailing newline that printf printed before the delimiter
				if strings.HasSuffix(text, "\n") {
					text = text[:len(text)-1]
				}
				buf.Reset()

				lastClipboardMu.Lock()
				changed := text != lastClipboardText
				if changed {
					lastClipboardText = text
				}
				lastClipboardMu.Unlock()

				if changed {
					log.Printf(">>> [Server] Clipboard changed instantly, broadcasting %d chars", len(text))
					broadcastJSON(map[string]interface{}{
						"type": "clipboard_get",
						"text": text,
					})
					if CaptureMode == CaptureModeAgent {
						BroadcastMsg(splitproto.Message{
							Type: splitproto.MsgClipboardGet,
							Config: map[string]interface{}{
								"text": text,
							},
						})
					}
				}
			} else {
				buf.WriteString(line + "\n")
			}
		}
	}()
}

// handleClipboardSet processes a clipboard_set message from the client.
// It sets the remote Wayland clipboard via wl-copy and optionally injects Ctrl+V
// for paste operations.
func handleClipboardSet(msg map[string]interface{}) {
	if !EnableClipboard {
		return
	}

	text, ok := msg["text"].(string)
	if !ok {
		return
	}

	log.Printf(">>> [Server] Setting remote clipboard: %d chars", len(text))
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(text)
	err := cmd.Run()
	if err != nil {
		log.Printf(">>> [Server] Error running wl-copy: %v", err)
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
		injectCtrlV()
	}
}

func injectCtrlV() {
	injectKey("ControlLeft", "keydown", 0)
	time.Sleep(10 * time.Millisecond)
	injectKey("KeyV", "keydown", 0)
	time.Sleep(10 * time.Millisecond)
	injectKey("KeyV", "keyup", 0)
	time.Sleep(10 * time.Millisecond)
	injectKey("ControlLeft", "keyup", 0)
}
