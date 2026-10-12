//go:build windows

package main

import "os/exec"

// openFolder shows dir in Explorer.
func openFolder(dir string) error { return exec.Command("explorer", dir).Start() }

// showFile shows the folder of file p in Explorer with p selected.
func showFile(p string) error { return exec.Command("explorer", "/select,", p).Start() }

// openURL opens url in the default browser.
func openURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
