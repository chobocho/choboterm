package main

import (
	"fmt"
	"io/fs"
	"os"

	"choboterm/frontend"
	"choboterm/internal/osutil"
	"choboterm/internal/version"
)

// appName is the window title prefix, e.g. "choboterm V0.1.0".
const appName = "choboterm V" + version.Version

func main() {
	// The same exe runs Lua scripts in a child process (see luahost.go).
	if len(os.Args) > 1 && os.Args[1] == "--lua-host" {
		os.Exit(runLuaHost(os.Stdin, os.Stdout))
	}
	opts, err := parseArgs(os.Args[1:])
	if err != nil || opts.help {
		osutil.AttachConsole()
		if err != nil {
			fmt.Fprintln(os.Stderr, "choboterm:", err)
		}
		fmt.Fprint(os.Stderr, usageText())
		if err != nil {
			os.Exit(2)
		}
		return
	}
	page, err := fs.Sub(frontend.Assets, frontend.AssetDir)
	if err != nil {
		panic(err) // embedded at build time
	}
	webPage = page

	if opts.headless || headlessBuild {
		if len(opts.unknown) > 0 {
			osutil.AttachConsole()
			fmt.Fprintf(os.Stderr, "choboterm: unknown option %q\n%s", opts.unknown[0], usageText())
			os.Exit(2)
		}
		os.Exit(runHeadless(opts))
	}
	runWindow(opts)
}
