//go:build !headless

package main

import (
	_ "embed"

	"choboterm/internal/app"
)

// The window icon on Linux (Windows takes it from the exe). It is embedded
// here because go:embed only reaches files below its own folder.
//
//go:embed build/appicon.png
var appIcon []byte

func init() {
	app.WindowIcon = appIcon
}
