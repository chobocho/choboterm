package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"choboterm/internal/version"
)

// runHeadless runs only the web server (--headless, or a headless build)
// until interrupted, and returns the exit code.
func runHeadless(opts cliOptions) int {
	attachConsole()
	app := NewApp()
	o := opts.webOptions()
	if o == nil {
		o = &webOptions{}
	}
	w, err := startWeb(app, webPage, *o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "choboterm:", err)
		if errors.Is(err, syscall.EADDRINUSE) {
			return 0
		}
		return 1
	}
	fmt.Printf("choboterm V%s web server (no window)\n\n  %s\n\n", version.Version, w.URL(false))
	if w.tls {
		fmt.Printf("  TLS certificate SHA-256: %s\n  (compare it with the one your browser shows before trusting it)\n\n", w.fp)
	}
	if opts.noToken {
		fmt.Println("WARNING: browser token authentication is disabled. Anyone who can reach this server can use its shells and saved sessions.")
	} else {
		fmt.Println("Anyone with this URL can use this PC's shells and saved sessions: keep it secret.")
	}
	fmt.Println("Ctrl+C stops the server.")
	if opts.open {
		if err := openURL(w.localURL()); err != nil {
			fmt.Fprintln(os.Stderr, "choboterm: open browser:", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("stopping...")
	w.stop()
	app.shutdown(nil)
	return 0
}
