package main

import (
	"choboterm/internal/config"
)

// GetSettings returns the user preferences.
func (a *App) GetSettings() config.Settings {
	return config.Load()
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
