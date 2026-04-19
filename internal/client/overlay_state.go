package client

func cloneOverlayState(state OverlayState) OverlayState {
	copyState := state
	if state.HUDLines != nil {
		copyState.HUDLines = append([]string(nil), state.HUDLines...)
	}
	if state.MenuItems != nil {
		copyState.MenuItems = append([]string(nil), state.MenuItems...)
	}
	return copyState
}
