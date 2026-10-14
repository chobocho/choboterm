//go:build !headless

package app

import (
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"choboterm/frontend"
	"choboterm/internal/config"
	"choboterm/internal/osutil"
)

// headlessBuild is true in a build without the window (-tags headless).
const headlessBuild = false

// The window icon on Linux (Windows takes it from the exe). Package main
// embeds it and sets it here: the picture is not below this folder.
var WindowIcon []byte

// runWindow runs the desktop window (and the web server with it, if asked).
func runWindow(opts cliOptions) {
	// Create an instance of the app structure
	app := NewApp()

	// The webOpts server starts with the window when asked on the command line
	// or in Settings > Web.
	webOpts := opts.webOptions()
	if !opts.web {
		webOpts = nil
		if s := config.Load().Web; s != nil && s.Enabled {
			webOpts = &webOptions{Addr: s.Addr}
		}
	}
	app.webStart = webOpts

	// With the web server, starting choboterm again shows the one already
	// running (its window may be hidden) instead of a second choboterm,
	// which couldn't serve the same port (see handoff.go).
	if webOpts != nil && handoffToRunning(*webOpts) {
		return
	}

	// The window starts hidden and is shown once it is moved to where it was
	// last time (see App.domReady), so it doesn't jump on screen.
	start := options.Normal
	if w := config.Load().Window; w != nil && w.Maximized {
		start = options.Maximised
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  appName,
		Width:  900,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
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
			WindowClassName:      osutil.WindowClass,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			// No blur: Acrylic turns solid gray whenever the window is inactive.
			BackdropType: windows.None,
		},
		// Translucency is Windows only; the page stays opaque on Linux.
		Linux: &linux.Options{
			Icon:             WindowIcon,
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
