package main

import (
	"choboterm/internal/dbg"
)

// DebugEnabled tells the frontend whether to send diagnostics.
func (a *App) DebugEnabled() bool {
	return dbg.Enabled()
}

// DebugLog writes a frontend diagnostic line.
func (a *App) DebugLog(msg string) {
	dbg.PrintJS(msg)
}
