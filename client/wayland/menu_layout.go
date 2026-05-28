package wayland

type MenuLayout struct {
	PanelX     int
	PanelY     int
	PanelW     int
	PanelH     int
	ItemsStart int
	ItemHeight int
}

func ComputeMenuLayout(width, height, itemCount int) MenuLayout {
	panelW := minInt(width-40, 640)
	if panelW < 280 {
		panelW = width
	}
	panelH := minInt(height-40, maxInt(160, 108+itemCount*22))
	if panelH < 120 {
		panelH = height
	}
	return MenuLayout{
		PanelX:     (width - panelW) / 2,
		PanelY:     (height - panelH) / 2,
		PanelW:     panelW,
		PanelH:     panelH,
		ItemsStart: 86,
		ItemHeight: 22,
	}
}

func menuItemIndexAt(width, height int, normalizedX, normalizedY float64, itemCount int) int {
	if itemCount == 0 {
		return -1
	}
	x := int(normalizedX * float64(width))
	y := int(normalizedY * float64(height))
	layout := ComputeMenuLayout(width, height, itemCount)
	if x < layout.PanelX || x > layout.PanelX+layout.PanelW {
		return -1
	}
	if y < layout.PanelY+layout.ItemsStart {
		return -1
	}
	idx := (y - (layout.PanelY + layout.ItemsStart)) / layout.ItemHeight
	if idx < 0 || idx >= itemCount {
		return -1
	}
	return idx
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
