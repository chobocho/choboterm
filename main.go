package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
)

//go:embed all:frontend/dist
var assets embed.FS

// AppVersion is shown in the window title. Keep in sync with wails.json productVersion.
const AppVersion = "0.2.17"

// appName is the window title prefix, e.g. "choboterm V0.1.0".
const appName = "choboterm V" + AppVersion

func main() {
	// The same exe runs Lua scripts in a child process (see luahost.go).
	if len(os.Args) > 1 && os.Args[1] == "--lua-host" {
		os.Exit(runLuaHost(os.Stdin, os.Stdout))
	}
	opts, err := parseArgs(os.Args[1:])
	if err != nil || opts.help {
		attachConsole()
		if err != nil {
			fmt.Fprintln(os.Stderr, "choboterm:", err)
		}
		fmt.Fprint(os.Stderr, usage)
		if err != nil {
			os.Exit(2)
		}
		return
	}
	page, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err) // embedded at build time
	}
	webPage = page

	if opts.headless || headlessBuild {
		if len(opts.unknown) > 0 {
			attachConsole()
			fmt.Fprintf(os.Stderr, "choboterm: unknown option %q\n%s", opts.unknown[0], usage)
			os.Exit(2)
		}
		os.Exit(runHeadless(opts))
	}
	runWindow(opts)
}
