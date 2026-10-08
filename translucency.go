package main

// BackgroundTranslucent reports whether the window was started see-through
// (Translucency "background"), so the page can draw see-through terminals.
func (a *App) BackgroundTranslucent() bool {
	return a.glass
}

// SetWindowOpacity makes the whole window, text included, percent opaque
// (100 = normal), like Xshell's or Tera Term's transparency. It does nothing
// in a window started see-through, which can't also be a layered window.
func (a *App) SetWindowOpacity(percent int) {
	if a.glass {
		return
	}
	setWindowAlpha(min(max(percent, 20), 100))
}
