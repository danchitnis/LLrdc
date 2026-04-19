package client

type menuLayout struct {
	panelX     int
	panelY     int
	panelW     int
	panelH     int
	itemsStart int
	itemHeight int
}

func computeMenuLayout(width, height, itemCount int) menuLayout {
	panelW := minInt(width-40, 640)
	if panelW < 280 {
		panelW = width
	}
	panelH := minInt(height-40, maxInt(160, 108+itemCount*22))
	if panelH < 120 {
		panelH = height
	}
	return menuLayout{
		panelX:     (width - panelW) / 2,
		panelY:     (height - panelH) / 2,
		panelW:     panelW,
		panelH:     panelH,
		itemsStart: 86,
		itemHeight: 22,
	}
}

func menuItemIndexAt(width, height int, normalizedX, normalizedY float64, itemCount int) int {
	if itemCount == 0 {
		return -1
	}
	x := int(normalizedX * float64(width))
	y := int(normalizedY * float64(height))
	layout := computeMenuLayout(width, height, itemCount)
	if x < layout.panelX || x > layout.panelX+layout.panelW {
		return -1
	}
	if y < layout.panelY+layout.itemsStart {
		return -1
	}
	idx := (y - (layout.panelY + layout.itemsStart)) / layout.itemHeight
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
