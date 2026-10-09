//go:build !windows

package main

import (
	"os/exec"
	"path/filepath"
)

// openFolder shows dir in the desktop's file manager.
func openFolder(dir string) error { return exec.Command("xdg-open", dir).Start() }

// showFile shows the folder of file p.
func showFile(p string) error { return openFolder(filepath.Dir(p)) }
