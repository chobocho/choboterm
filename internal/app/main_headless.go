//go:build headless

package app

// headlessBuild is true in a build without the window (-tags headless):
// it only runs the web server, and needs no GTK/WebKit (Linux) or WebView2.
const headlessBuild = true

func runWindow(cliOptions) {}
