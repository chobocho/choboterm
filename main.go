package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// AppVersion is shown in the window title. Keep in sync with wails.json productVersion.
const AppVersion = "0.2.0"

// appName is the window title prefix, e.g. "choboterm V0.1.0".
const appName = "choboterm V" + AppVersion

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// The window starts hidden and is shown once it is moved to where it was
	// last time (see App.domReady), so it doesn't jump on screen.
	start := options.Normal
	settings := loadSettings()
	if w := settings.Window; w != nil && w.Maximized {
		start = options.Maximised
	}

	// "Only the background" translucency needs a see-through window from the
	// start; the page then draws the terminals with see-through backgrounds.
	win := &windows.Options{WindowClassName: windowClass}
	background := &options.RGBA{R: 0, G: 0, B: 0, A: 1}
	if settings.Translucency == "background" {
		app.glass = true
		win.WebviewIsTransparent = true
		win.WindowIsTranslucent = true
		// No blur: Acrylic turns solid gray whenever the window is inactive.
		win.BackdropType = windows.None
		background = &options.RGBA{R: 0, G: 0, B: 0, A: 0}
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  appName,
		Width:  900,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: background,
		StartHidden:      true,
		WindowStartState: start,
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Windows:          win,
		// Files dropped on the file window are uploaded (see files.ts).
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
