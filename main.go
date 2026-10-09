package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// AppVersion is shown in the window title. Keep in sync with wails.json productVersion.
const AppVersion = "0.2.5"

// appName is the window title prefix, e.g. "choboterm V0.1.0".
const appName = "choboterm V" + AppVersion

func main() {
	// The same exe runs Lua scripts in a child process (see luahost.go).
	if len(os.Args) > 1 && os.Args[1] == "--lua-host" {
		os.Exit(runLuaHost(os.Stdin, os.Stdout))
	}

	// Create an instance of the app structure
	app := NewApp()

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
