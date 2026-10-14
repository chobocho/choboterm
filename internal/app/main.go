package app

import (
	"fmt"
	"io/fs"
	"os"

	"choboterm/frontend"
	"choboterm/internal/config"
	"choboterm/internal/luahost"
	"choboterm/internal/osutil"
	"choboterm/internal/version"
)

// appName is the window title prefix, e.g. "choboterm V0.1.0".
const appName = "choboterm V" + version.Version

// Main runs choboterm with the command line of the process: the window, the
// web server without it (--headless), or a Lua script (--lua-host, started by
// choboterm itself). It is all package main does.
func Main() {
	// The same exe runs Lua scripts in a child process (see luahost.go).
	if len(os.Args) > 1 && os.Args[1] == "--lua-host" {
		os.Exit(luahost.Run(os.Stdin, os.Stdout))
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
	// Until the settings are first saved, the terminal font size is the
	// desktop's (GNOME Terminal on Ubuntu: 13pt, 17px) rather than 15px.
	config.FirstRun = func(s *config.Settings) {
		if f := osutil.SystemMonoFont(); f.Size >= 8 && f.Size <= 40 {
			s.FontSize = f.Size
		}
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
