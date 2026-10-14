package app

import (
	"choboterm/internal/config"
	"choboterm/internal/osutil"
)

// GetSettings returns the user preferences.
func (a *App) GetSettings() config.Settings {
	return config.Load()
}

// GetSystemFont is the desktop's fixed-width font (Linux: GNOME or KDE), which
// the page tries before its Korean fonts so that text looks as in the
// desktop's own terminal. Empty elsewhere.
func (a *App) GetSystemFont() osutil.MonoFont {
	return osutil.SystemMonoFont()
}

// SaveSettings stores the user preferences. The window position, the
// update check state and the web server's settings are kept as stored.
func (a *App) SaveSettings(s config.Settings) error {
	return config.Update(func(cur *config.Settings) {
		w, u, web := cur.Window, cur.Update, cur.Web
		*cur = s
		cur.Window, cur.Update, cur.Web = w, u, web
	})
}
