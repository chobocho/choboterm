package main

import (
	_ "embed"

	"choboterm/internal/i18n"
)

// GetLanguage tells the page which language to show.
func (a *App) GetLanguage() string {
	return i18n.Lang()
}
