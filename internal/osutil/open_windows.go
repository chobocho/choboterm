//go:build windows

package osutil

import "os/exec"

// OpenFolder shows dir in Explorer.
func OpenFolder(dir string) error { return exec.Command("explorer", dir).Start() }

// ShowFile shows the folder of file p in Explorer with p selected.
func ShowFile(p string) error { return exec.Command("explorer", "/select,", p).Start() }

// OpenURL opens url in the default browser.
func OpenURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
