package client

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func (a *NativeApp) handleRendererInput(msg map[string]any) error {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "mousemove", "mousebtn", "keydown", "keyup", "wheel":
		a.session.RecordLocalInput(LocalInputSample{
			Type:   msgType,
			Action: stringFromAny(msg["action"]),
			Button: intFromAny(msg["button"]),
			Key:    stringFromAny(msg["key"]),
			X:      floatOrZero(msg["x"]),
			Y:      floatOrZero(msg["y"]),
			AtMs:   time.Now().UnixMilli(),
		})
	}
	switch msgType {
	case "resize":
		width, height := intFromAny(msg["width"]), intFromAny(msg["height"])
		if width > 0 && height > 0 {
			a.updateResolutionIndex(width, height)
			a.refreshOverlay()
			width, height = a.targetStreamSize(width, height)
			return a.session.SendResize(width, height)
		}
		return nil
	case "keydown", "keyup":
		handled, err := a.handleKeyMessage(msgType, stringFromAny(msg["key"]))
		if handled {
			return err
		}
		if a.isMenuVisible() {
			return nil
		}
	case "mousemove", "mousebtn", "wheel":
		if a.isMenuVisible() {
			return a.handleMenuPointerInput(msg)
		}
	}
	return a.session.SendInput(msg)
}

func (a *NativeApp) handleMenuPointerInput(msg map[string]any) error {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "mousemove":
		x, okX := floatFromAny(msg["x"])
		y, okY := floatFromAny(msg["y"])
		if !okX || !okY {
			return nil
		}
		a.updateMenuHover(x, y)
		return nil
	case "mousebtn":
		x, okX := floatFromAny(msg["x"])
		y, okY := floatFromAny(msg["y"])
		if !okX || !okY {
			a.mu.RLock()
			x = a.lastMenuMouseX
			y = a.lastMenuMouseY
			a.mu.RUnlock()
		} else {
			a.updateMenuHover(x, y)
		}
		button := intFromAny(msg["button"])
		action, _ := msg["action"].(string)
		if button == 0 && action == "mouseup" {
			return a.activateMenuPointer(x, y)
		}
		return nil
	default:
		return nil
	}
}

func (a *NativeApp) handleKeyMessage(msgType, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}

	switch msgType {
	case "keydown":
		a.setModifier(key, true)
		if a.isMenuToggleKey(key) {
			a.toggleMenu()
			return true, nil
		}
		if a.isMenuVisible() {
			switch key {
			case "Escape":
				a.dismissMenuLevel()
			case "ArrowUp":
				a.moveMenuSelection(-1)
			case "ArrowDown", "Tab":
				a.moveMenuSelection(1)
			case "ArrowLeft":
				a.collapseCurrentSubmenu()
			case "ArrowRight":
				return true, a.executeSelectedMenuItem()
			case "Enter", "Space":
				return true, a.executeSelectedMenuItem()
			}
			return true, nil
		}
	case "keyup":
		a.setModifier(key, false)
		if key == "F1" {
			return true, nil
		}
		if key == "Comma" && a.menuShortcutModifier() {
			return true, nil
		}
	}
	return false, nil
}

func (a *NativeApp) isMenuToggleKey(key string) bool {
	if key == "F1" {
		return true
	}
	return key == "Comma" && a.menuShortcutModifier()
}

func (a *NativeApp) menuShortcutModifier() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if runtime.GOOS == "darwin" {
		return a.modifiers["MetaLeft"] || a.modifiers["MetaRight"]
	}
	return a.modifiers["ControlLeft"] || a.modifiers["ControlRight"]
}

func (a *NativeApp) setModifier(key string, value bool) {
	switch key {
	case "ControlLeft", "ControlRight", "MetaLeft", "MetaRight":
		a.mu.Lock()
		a.modifiers[key] = value
		a.mu.Unlock()
	}
}

func (a *NativeApp) moveMenuSelection(delta int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := a.visibleMenuItemsLocked()
	if len(items) == 0 {
		a.menuSelected = 0
		return
	}
	idx := a.menuSelected
	for step := 0; step < len(items); step++ {
		idx = (idx + delta + len(items)) % len(items)
		if items[idx].Enabled {
			a.menuSelected = idx
			break
		}
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) updateMenuHover(x, y float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastMenuMouseX = x
	a.lastMenuMouseY = y
	if a.renderer == nil {
		return
	}
	width, height := a.renderer.Size()
	items := a.visibleMenuItemsLocked()
	idx := menuItemIndexAt(width, height, x, y, len(items))
	if idx >= 0 && idx < len(items) && items[idx].Enabled {
		a.menuSelected = idx
		a.refreshOverlayLocked()
	}
}

func (a *NativeApp) activateMenuPointer(x, y float64) error {
	a.mu.RLock()
	if a.renderer == nil {
		a.mu.RUnlock()
		return nil
	}
	width, height := a.renderer.Size()
	items := a.visibleMenuItemsLocked()
	a.mu.RUnlock()

	idx := menuItemIndexAt(width, height, x, y, len(items))
	if idx < 0 || idx >= len(items) || !items[idx].Enabled {
		return nil
	}

	a.mu.Lock()
	a.menuSelected = idx
	a.refreshOverlayLocked()
	a.mu.Unlock()
	return a.ExecuteCommand(items[idx].ID)
}

func (a *NativeApp) executeSelectedMenuItem() error {
	a.mu.RLock()
	items := a.visibleMenuItemsLocked()
	selected := a.menuSelected
	a.mu.RUnlock()
	if selected < 0 || selected >= len(items) {
		return nil
	}
	if !items[selected].Enabled {
		return nil
	}
	if items[selected].Expandable {
		return a.ExecuteCommand(items[selected].ID)
	}
	return a.ExecuteCommand(items[selected].ID)
}

func (a *NativeApp) ExecuteCommand(id string) error {
	id = strings.TrimSpace(id)
	switch id {
	case "menu.toggle":
		a.toggleMenu()
		return nil
	case "menu.up":
		a.moveMenuSelection(-1)
		return nil
	case "menu.down":
		a.moveMenuSelection(1)
		return nil
	case "menu.select":
		return a.executeSelectedMenuItem()
	case "quit":
		a.requestShutdown("menu_quit")
		return nil
	default:
		return a.executeValueCommand(id)
	}
}

func (a *NativeApp) executeValueCommand(id string) error {
	switch {
	case strings.HasSuffix(id, ".menu"):
		a.toggleSubmenu(id)
		return nil
	case strings.HasPrefix(id, "codec.set:"):
		return a.setCodec(strings.TrimPrefix(id, "codec.set:"))
	case strings.HasPrefix(id, "framerate.set:"):
		return a.setFramerate(strings.TrimPrefix(id, "framerate.set:"))
	case strings.HasPrefix(id, "resolution.set:"):
		return a.setResolution(strings.TrimPrefix(id, "resolution.set:"))
	case strings.HasPrefix(id, "hdpi.set:"):
		return a.setHDPI(strings.TrimPrefix(id, "hdpi.set:"))
	case strings.HasPrefix(id, "stats.set:"):
		return a.setStats(strings.TrimPrefix(id, "stats.set:"))
	case strings.HasPrefix(id, "latency.set:"):
		return a.setLatencyProbe(strings.TrimPrefix(id, "latency.set:"))
	case strings.HasPrefix(id, "cursor.set:"):
		return a.setDebugCursor(strings.TrimPrefix(id, "cursor.set:"))
	default:
		return fmt.Errorf("unknown command: %s", id)
	}
}

func (a *NativeApp) setCodec(value string) error {
	a.mu.Lock()
	target := -1
	for idx, option := range a.codecOptions {
		if option.Value == value {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown codec option: %s", value)
	}
	a.codecIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if value != "" || a.session.State().Connected {
		if err := a.session.SendConfig(map[string]any{"videoCodec": value}); err != nil && !strings.Contains(err.Error(), "not connected") {
			return err
		}
	}
	return nil
}

func (a *NativeApp) setFramerate(value string) error {
	targetValue := intFromString(value)
	a.mu.Lock()
	target := -1
	for idx, option := range a.framerateOptions {
		if option.Value == targetValue {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown framerate option: %s", value)
	}
	a.framerateIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"framerate": targetValue}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	return nil
}

func (a *NativeApp) setResolution(label string) error {
	a.mu.Lock()
	target := -1
	var preset resolutionOption
	for idx, option := range a.resolutionOpts {
		if option.Label == label {
			target = idx
			preset = option
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown resolution option: %s", label)
	}
	a.resolutionIndex = target
	a.refreshOverlayLocked()
	windowWidth := 0
	windowHeight := 0
	if a.renderer != nil {
		windowWidth, windowHeight = a.renderer.Size()
	}
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"max_res": preset.Value}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	if windowWidth > 0 && windowHeight > 0 {
		targetWidth, targetHeight := targetStreamSizeForResolution(windowWidth, windowHeight, preset.Value)
		if err := a.session.SendResize(targetWidth, targetHeight); err != nil && !strings.Contains(err.Error(), "not connected") {
			return err
		}
	}
	return nil
}

func (a *NativeApp) setHDPI(value string) error {
	targetValue := intFromString(value)
	a.mu.Lock()
	target := -1
	for idx, option := range a.hdpiOptions {
		if option.Value == targetValue {
			target = idx
			break
		}
	}
	if target < 0 {
		a.mu.Unlock()
		return fmt.Errorf("unknown hdpi option: %s", value)
	}
	a.hdpiIndex = target
	a.refreshOverlayLocked()
	a.mu.Unlock()

	if err := a.session.SendConfig(map[string]any{"hdpi": targetValue}); err != nil && !strings.Contains(err.Error(), "not connected") {
		return err
	}
	return nil
}

func (a *NativeApp) setStats(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.showStats = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	return nil
}

func (a *NativeApp) setLatencyProbe(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.latencyProbe = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	if a.renderer != nil {
		a.renderer.SetLatencyProbe(enabled)
	}
	return nil
}

func (a *NativeApp) setDebugCursor(value string) error {
	enabled, err := parseOnOff(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.debugCursor = enabled
	a.refreshOverlayLocked()
	a.mu.Unlock()
	if a.renderer != nil {
		a.renderer.SetDebugCursor(enabled)
	}
	return nil
}

func (a *NativeApp) updateResolutionIndex(width, height int) {
	_ = width
	_ = height
}

func (a *NativeApp) toggleSubmenu(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu == id {
		a.currentSubmenu = ""
	} else {
		a.currentSubmenu = id
	}
	items := a.visibleMenuItemsLocked()
	a.clampMenuSelectionLocked(items)
	a.refreshOverlayLocked()
}

func (a *NativeApp) dismissMenuLevel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu != "" {
		parentID := a.currentSubmenu
		a.currentSubmenu = ""
		items := a.visibleMenuItemsLocked()
		for idx, item := range items {
			if item.ID == parentID {
				a.menuSelected = idx
				break
			}
		}
		a.refreshOverlayLocked()
		return
	}
	a.menuVisible = false
	a.refreshOverlayLocked()
}

func (a *NativeApp) collapseCurrentSubmenu() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentSubmenu == "" {
		return
	}
	parentID := a.currentSubmenu
	a.currentSubmenu = ""
	items := a.visibleMenuItemsLocked()
	for idx, item := range items {
		if item.ID == parentID {
			a.menuSelected = idx
			break
		}
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) clampMenuSelectionLocked(items []MenuItemSnapshot) {
	if len(items) == 0 {
		a.menuSelected = 0
		return
	}
	if a.menuSelected < 0 || a.menuSelected >= len(items) || !items[a.menuSelected].Enabled {
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(items)
	}
}

func (a *NativeApp) toggleMenu() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuVisible = !a.menuVisible
	if a.menuVisible {
		a.currentSubmenu = ""
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(a.visibleMenuItemsLocked())
	} else {
		a.currentSubmenu = ""
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) setMenuVisible(visible bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuVisible = visible
	if visible {
		a.currentSubmenu = ""
		a.menuSelected = a.firstEnabledVisibleMenuItemLocked(a.visibleMenuItemsLocked())
	} else {
		a.currentSubmenu = ""
	}
	a.refreshOverlayLocked()
}

func (a *NativeApp) isMenuVisible() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.menuVisible
}

func (a *NativeApp) firstEnabledVisibleMenuItemLocked(items []MenuItemSnapshot) int {
	for idx, item := range items {
		if item.Enabled {
			return idx
		}
	}
	return 0
}

func (a *NativeApp) menuItemsLocked() []MenuItemSnapshot {
	codec := a.codecOptions[a.codecIndex]
	framerate := a.framerateOptions[a.framerateIndex]
	resolution := a.resolutionOpts[a.resolutionIndex]
	hdpi := a.hdpiOptions[a.hdpiIndex]
	return []MenuItemSnapshot{
		{ID: "codec.menu", Label: "Codec", Value: codec.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "codec.menu"},
		{ID: "framerate.menu", Label: "Frame Rate", Value: framerate.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "framerate.menu"},
		{ID: "resolution.menu", Label: "Max Resolution", Value: resolution.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "resolution.menu"},
		{ID: "hdpi.menu", Label: "HDPI", Value: hdpi.Label, Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "hdpi.menu"},
		{ID: "stats.menu", Label: "Stats HUD", Value: onOff(a.showStats), Enabled: true, Expandable: true, Expanded: a.currentSubmenu == "stats.menu"},
		{ID: "quit", Label: "Quit", Enabled: true},
	}
}

func (a *NativeApp) visibleMenuItemsLocked() []MenuItemSnapshot {
	baseItems := a.menuItemsLocked()
	items := make([]MenuItemSnapshot, 0, len(baseItems)+12)
	for _, item := range baseItems {
		items = append(items, item)
		if !item.Expanded {
			continue
		}
		switch item.ID {
		case "codec.menu":
			for idx, option := range a.codecOptions {
				items = append(items, MenuItemSnapshot{
					ID:       "codec.set:" + option.Value,
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.codecIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "framerate.menu":
			for idx, option := range a.framerateOptions {
				items = append(items, MenuItemSnapshot{
					ID:       fmt.Sprintf("framerate.set:%d", option.Value),
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.framerateIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "resolution.menu":
			for idx, option := range a.resolutionOpts {
				items = append(items, MenuItemSnapshot{
					ID:       "resolution.set:" + option.Label,
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.resolutionIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "hdpi.menu":
			for idx, option := range a.hdpiOptions {
				items = append(items, MenuItemSnapshot{
					ID:       fmt.Sprintf("hdpi.set:%d", option.Value),
					Label:    option.Label,
					Enabled:  true,
					Current:  idx == a.hdpiIndex,
					Depth:    1,
					ParentID: item.ID,
				})
			}
		case "stats.menu":
			items = append(items, a.booleanMenuItemsLocked(item.ID, "stats")...)
		}
	}
	return items
}

func (a *NativeApp) booleanMenuItemsLocked(parentID, prefix string) []MenuItemSnapshot {
	currentOn := false
	switch prefix {
	case "stats":
		currentOn = a.showStats
	case "latency":
		currentOn = a.latencyProbe
	case "cursor":
		currentOn = a.debugCursor
	}
	return []MenuItemSnapshot{
		{ID: prefix + ".set:on", Label: "On", Enabled: true, Current: currentOn, Depth: 1, ParentID: parentID},
		{ID: prefix + ".set:off", Label: "Off", Enabled: true, Current: !currentOn, Depth: 1, ParentID: parentID},
	}
}

func onOff(v bool) string {
	if v {
		return "On"
	}
	return "Off"
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean option: %s", value)
	}
}

func (a *NativeApp) MenuSnapshot() any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	items := a.visibleMenuItemsLocked()
	for idx := range items {
		items[idx].Selected = idx == a.menuSelected
	}
	return MenuStateSnapshot{
		Visible:       a.menuVisible,
		Title:         "LLRDC NATIVE MENU",
		Hint:          a.menuHintLocked(),
		SelectedIndex: a.menuSelected,
		Items:         items,
	}
}

func (a *NativeApp) menuHintLocked() string {
	server := a.desiredServerURL
	if server == "" {
		server = "<not set>"
	}
	modifier := "CTRL+,"
	if runtime.GOOS == "darwin" {
		modifier = "CMD+,"
	}
	return fmt.Sprintf("SERVER %s | BUILD %s | %s / F1 TOGGLE | ENTER OPEN/SELECT | LEFT BACK | ESC CLOSE", strings.ToUpper(server), strings.ToUpper(a.opts.BuildID), modifier)
}
