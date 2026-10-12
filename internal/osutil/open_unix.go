//go:build !windows

package osutil

import (
	"os/exec"
	"path/filepath"
)

// OpenFolder shows dir in the desktop's file manager.
func OpenFolder(dir string) error { return exec.Command("xdg-open", dir).Start() }

// ShowFile shows the folder of file p.
func ShowFile(p string) error { return OpenFolder(filepath.Dir(p)) }

// OpenURL opens url in the default browser.
func OpenURL(url string) error { return exec.Command("xdg-open", url).Start() }
