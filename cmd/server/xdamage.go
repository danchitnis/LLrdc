package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"log"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/damage"
	"github.com/jezek/xgb/xproto"
)

var (
	damageTrackerMutex sync.Mutex
	dirtyTiles         = make(map[string]image.Rectangle)
	settleTimer        *time.Timer
	xgbConnDamage      *xgb.Conn
	damageRootWin      xproto.Window
	SettleTime         = 300
	sendingPatches     bool

	clearMutex sync.Mutex
	clearRects []map[string]interface{}
	clearTimer *time.Timer
)

func SetTileSize(size int) {
	damageTrackerMutex.Lock()
	defer damageTrackerMutex.Unlock()
	if size < 64 {
		size = 64
	}
	if size > 1024 {
		size = 1024
	}
	if TileSize == size {
		return
	}
	log.Printf("Tile size changed from %d to %d, clearing pending patches", TileSize, size)
	TileSize = size
	dirtyTiles = make(map[string]image.Rectangle)
	broadcastJSON(map[string]interface{}{
		"type": "clear_lossless",
	})
}

func SetSettleTime(ms int) {
	damageTrackerMutex.Lock()
	defer damageTrackerMutex.Unlock()
	if ms < 100 {
		ms = 100
	}
	SettleTime = ms
	log.Printf("Settle time changed to %dms", SettleTime)
}

func SetEnableHybrid(enable bool) {
	damageTrackerMutex.Lock()
	defer damageTrackerMutex.Unlock()
	if EnableHybrid == enable {
		return
	}
	EnableHybrid = enable
	if !enable {
		// Clear everything if disabled
		dirtyTiles = make(map[string]image.Rectangle)
		if settleTimer != nil {
			settleTimer.Stop()
		}
		broadcastJSON(map[string]interface{}{
			"type": "clear_lossless",
		})
	}
}

func initDamageTracking(display string) {
	log.Println("Starting initDamageTracking...")
	var err error
	xgbConnDamage, err = xgb.NewConnDisplay(display)
	if err != nil {
		log.Printf("Failed to connect to X for damage tracking: %v", err)
		return
	}

	err = damage.Init(xgbConnDamage)
	if err != nil {
		log.Printf("Failed to init XDamage extension: %v", err)
		return
	}
	damage.QueryVersion(xgbConnDamage, 1, 1).Reply()

	setup := xproto.Setup(xgbConnDamage)
	damageRootWin = setup.DefaultScreen(xgbConnDamage).Root

	dmgId, err := damage.NewDamageId(xgbConnDamage)
	if err != nil {
		log.Printf("Failed to create damage ID: %v", err)
		return
	}

	log.Printf("Creating Damage with dmgId=%v on rootWin=%v", dmgId, damageRootWin)
	err = damage.CreateChecked(xgbConnDamage, dmgId, xproto.Drawable(damageRootWin), damage.ReportLevelRawRectangles).Check()
	if err != nil {
		log.Printf("Failed to create damage object: %v", err)
		return
	}

	dmgChan := make(chan image.Rectangle, 1000)
	go func() {
		for rect := range dmgChan {
			handleDamage(rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
		}
	}()

	go func() {
		for {
			ev, err := xgbConnDamage.WaitForEvent()
			if err != nil {
				log.Printf("XGB WaitForEvent error: %v", err)
				return
			}
			if ev == nil {
				return
			}
			switch e := ev.(type) {
			case damage.NotifyEvent:
				damage.Subtract(xgbConnDamage, e.Damage, 0, 0)
				select {
				case dmgChan <- image.Rect(int(e.Area.X), int(e.Area.Y), int(e.Area.X)+int(e.Area.Width), int(e.Area.Y)+int(e.Area.Height)):
				default:
					// Drop if channel full to prevent blocking event loop
				}
			}
		}
	}()
	log.Println("XDamage tracking initialized.")
}

func queueClear(x, y, w, h int) {
	clearMutex.Lock()
	clearRects = append(clearRects, map[string]interface{}{
		"x": x,
		"y": y,
		"w": w,
		"h": h,
	})
	if clearTimer == nil {
		clearTimer = time.AfterFunc(33*time.Millisecond, flushClears)
	}
	clearMutex.Unlock()
}

func flushClears() {
	clearMutex.Lock()
	rects := clearRects
	clearRects = nil
	clearTimer = nil
	clearMutex.Unlock()

	if len(rects) > 0 {
		broadcastJSON(map[string]interface{}{
			"type":  "clear_lossless",
			"rects": rects,
		})
	}
}

func handleDamage(x, y, w, h int) {
	if !EnableHybrid {
		return
	}

	damageTrackerMutex.Lock()
	defer damageTrackerMutex.Unlock()

	// Intersect with tiles
	for ty := (y / TileSize) * TileSize; ty < y+h; ty += TileSize {
		for tx := (x / TileSize) * TileSize; tx < x+w; tx += TileSize {
			key := fmt.Sprintf("%d,%d", tx, ty)
			if _, exists := dirtyTiles[key]; !exists {
				dirtyTiles[key] = image.Rect(tx, ty, tx+TileSize, ty+TileSize)
				// Queue clear for this specific newly dirty tile
				queueClear(tx, ty, TileSize, TileSize)
			}
		}
	}

	if settleTimer == nil {
		settleTimer = time.AfterFunc(time.Duration(SettleTime)*time.Millisecond, sendLosslessPatches)
	} else {
		settleTimer.Reset(time.Duration(SettleTime) * time.Millisecond)
	}
}

func sendLosslessPatches() {
	damageTrackerMutex.Lock()
	if !EnableHybrid {
		dirtyTiles = make(map[string]image.Rectangle)
		damageTrackerMutex.Unlock()
		return
	}
	if sendingPatches {
		damageTrackerMutex.Unlock()
		if settleTimer != nil {
			settleTimer.Reset(100 * time.Millisecond)
		}
		return
	}
	tiles := dirtyTiles
	dirtyTiles = make(map[string]image.Rectangle)
	sendingPatches = true
	damageTrackerMutex.Unlock()

	defer func() {
		damageTrackerMutex.Lock()
		sendingPatches = false
		damageTrackerMutex.Unlock()
	}()

	if len(tiles) == 0 {
		return
	}

	totalPixels := len(tiles) * TileSize * TileSize
	if totalPixels > 8000000 { // Approx 4K screen
		log.Printf("Dropping lossless patches because total area (%d px) exceeded limit", totalPixels)
		// Large screen changes are handled efficiently by the video stream.
		// Sending dozens of huge PNGs would flood the WebSocket and freeze the client cursor.
		return
	}

	// Fetch actual current screen size to avoid BadMatch
	geom, err := xproto.GetGeometry(xgbConnDamage, xproto.Drawable(damageRootWin)).Reply()
	if err != nil {
		log.Printf("GetGeometry failed: %v", err)
		return
	}
	screenWidth := int(geom.Width)
	screenHeight := int(geom.Height)

	patchesSent := 0
	for _, rect := range tiles {
		// Clip to screen
		if rect.Min.X >= screenWidth || rect.Min.Y >= screenHeight {
			continue
		}
		if rect.Max.X > screenWidth {
			rect.Max.X = screenWidth
		}
		if rect.Max.Y > screenHeight {
			rect.Max.Y = screenHeight
		}

		w := rect.Dx()
		h := rect.Dy()
		if w <= 0 || h <= 0 {
			continue
		}

		imgReply, err := xproto.GetImage(xgbConnDamage, xproto.ImageFormatZPixmap, xproto.Drawable(damageRootWin), int16(rect.Min.X), int16(rect.Min.Y), uint16(w), uint16(h), ^uint32(0)).Reply()
		if err != nil {
			// Silently skip if screen changed under us
			continue
		}

		rgba := image.NewNRGBA(image.Rect(0, 0, w, h))
		stride := w * 4
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := y*stride + x*4
				if i+3 < len(imgReply.Data) {
					// BGRA to RGBA
					rgba.Pix[i] = imgReply.Data[i+2]
					rgba.Pix[i+1] = imgReply.Data[i+1]
					rgba.Pix[i+2] = imgReply.Data[i]
					rgba.Pix[i+3] = 255
				}
			}
		}

		var buf bytes.Buffer
		err = png.Encode(&buf, rgba)
		if err != nil {
			continue
		}

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

		msg := map[string]interface{}{
			"type": "lossless_patch",
			"x":    rect.Min.X,
			"y":    rect.Min.Y,
			"w":    w,
			"h":    h,
			"data": "data:image/png;base64," + b64,
		}

		broadcastJSON(msg)
		patchesSent++
		if patchesSent%5 == 0 {
			time.Sleep(5 * time.Millisecond) // Yield to allow cursor/websocket traffic to process smoothly
		}
	}
}
