package main

import (
	"os/exec"

	"choboterm/internal/localshell"
)

// GetLocalShells lists the local shells installed on this PC.
func (a *App) GetLocalShells() []localshell.LocalShell {
	var list []localshell.LocalShell
	for _, s := range localshell.Shells {
		if _, err := exec.LookPath(s.Exe); err == nil {
			list = append(list, localshell.LocalShell{Name: s.Name, Label: s.Label})
		}
	}
	return list
}
