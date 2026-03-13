package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
)

var lastCursorSerial uint32
var cursorMutex sync.Mutex
var cachedCursorMsg map[string]interface{}

func startCursorWatcher(display string) {
	go func() {
		var X *xgb.Conn
		var err error

		// Retry connecting to X and initializing xfixes
		for i := 0; i < 10; i++ {
			time.Sleep(2 * time.Second)
			X, err = xgb.NewConnDisplay(display)
			if err != nil {
				log.Printf("Cursor watcher attempt %d: failed to connect to X: %v", i+1, err)
				continue
			}

			err = xfixes.Init(X)
			if err != nil {
				log.Printf("Cursor watcher attempt %d: failed to init xfixes: %v", i+1, err)
				X.Close()
				continue
			}

			// Check version
			v, err := xfixes.QueryVersion(X, 5, 0).Reply()
			if err != nil {
				log.Printf("Cursor watcher attempt %d: failed to query xfixes version: %v", i+1, err)
				X.Close()
				continue
			}
			log.Printf("XFixes version: %d.%d", v.MajorVersion, v.MinorVersion)

			setup := xproto.Setup(X)
			if len(setup.Roots) == 0 {
				log.Printf("Cursor watcher attempt %d: no roots found", i+1)
				X.Close()
				continue
			}
			root := setup.Roots[0].Root

			err = xfixes.SelectCursorInputChecked(X, root, xfixes.CursorNotifyMaskDisplayCursor).Check()
			if err != nil {
				log.Printf("Cursor watcher attempt %d: failed to select cursor input: %v", i+1, err)
				X.Close()
				continue
			}

			log.Printf("Cursor watcher successfully initialized on %s", display)
			break
		}

		if err != nil {
			log.Printf("Cursor watcher failed to initialize after retries")
			return
		}
		defer X.Close()

		log.Println("Cursor watcher started successfully")

		// Get initial cursor
		updateCursor(X)

		for {
			ev, err := X.WaitForEvent()
			if err != nil {
				log.Printf("Cursor watcher error waiting for event: %v", err)
				return
			}
			if ev == nil {
				break
			}

			switch ev.(type) {
			case xfixes.CursorNotifyEvent:
				updateCursor(X)
			}
		}
	}()
}

func updateCursor(X *xgb.Conn) {
	cookie := xfixes.GetCursorImage(X)
	reply, err := cookie.Reply()
	if err != nil {
		log.Printf("Failed to get cursor image: %v", err)
		return
	}

	if reply.CursorSerial == lastCursorSerial {
		return
	}
	lastCursorSerial = reply.CursorSerial

	width := int(reply.Width)
	height := int(reply.Height)

	// Don't process empty cursors
	if width <= 0 || height <= 0 {
		return
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i, p := range reply.CursorImage {
		a := uint8(p >> 24)
		// X11 ARGB pixels are premultiplied, but we can usually just extract the color.
		// To be safe with NRGBA we extract it directly.
		r := uint8((p >> 16) & 0xff)
		g := uint8((p >> 8) & 0xff)
		b := uint8(p & 0xff)

		// Un-premultiply if alpha > 0 and alpha < 255
		if a > 0 && a < 255 {
			r = uint8((uint32(r) * 255) / uint32(a))
			g = uint8((uint32(g) * 255) / uint32(a))
			b = uint8((uint32(b) * 255) / uint32(a))
		}

		img.SetNRGBA(i%width, i/width, color.NRGBA{R: r, G: g, B: b, A: a})
	}

	var buf bytes.Buffer
	err = png.Encode(&buf, img)
	if err != nil {
		log.Printf("Failed to encode cursor to png: %v", err)
		return
	}

	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	dataURL := fmt.Sprintf("data:image/png;base64,%s", b64)

	msg := map[string]interface{}{
		"type":    "cursor_shape",
		"dataURL": dataURL,
		"xhot":    reply.Xhot,
		"yhot":    reply.Yhot,
	}

	cursorMutex.Lock()
	cachedCursorMsg = msg
	cursorMutex.Unlock()

	broadcastJSON(msg)
}
