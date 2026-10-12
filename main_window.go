//go:build !headless

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// headlessBuild is true in a build without the window (-tags headless).
const headlessBuild = false

// The window icon on Linux (Windows takes it from the exe).
//
//go:embed build/appicon.png
var appIcon []byte

// runWindow runs the desktop window (and the web server with it, if asked).
func runWindow(opts cliOptions) {
	// Create an instance of the app structure
	app := NewApp()

	// The web server starts with the window when asked on the command line
	// or in Settings > Web.
	web := opts.webOptions()
	if !opts.web {
		web = nil
		if s := loadSettings().Web; s != nil && s.Enabled {
			web = &webOptions{Addr: s.Addr}
		}
	}
	app.webStart = web

	// With the web server, starting choboterm again shows the one already
	// running (its window may be hidden) instead of a second choboterm,
	// which couldn't serve the same port (see handoff.go).
	if web != nil && handoffToRunning(*web) {
		return
	}

	// The window starts hidden and is shown once it is moved to where it was
	// last time (see App.domReady), so it doesn't jump on screen.
	start := options.Normal
	if w := loadSettings().Window; w != nil && w.Maximized {
		start = options.Maximised
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  appName,
		Width:  900,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// The window is see-through wherever the page is, so translucency
		// (Settings) can be switched on and off without a restart. The page
		// paints an opaque background while it is off.
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		StartHidden:      true,
		WindowStartState: start,
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Windows: &windows.Options{
			WindowClassName:      windowClass,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			// No blur: Acrylic turns solid gray whenever the window is inactive.
			BackdropType: windows.None,
		},
		// Translucency is Windows only; the page stays opaque on Linux.
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "choboterm",
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
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
