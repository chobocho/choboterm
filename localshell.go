package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LocalShell is a program on this PC that can run in a tab instead of a connection.
type LocalShell struct {
	Name  string `json:"name"`  // what goes in the Host field: "cmd", "powershell", "wsl", "bash"...
	Label string `json:"label"` // for the list: "명령 프롬프트 (cmd)"...
}

// localCommand returns the command line for a local shell named in the Host
// field ("cmd", "powershell", "wsl", "wsl -d Ubuntu", "pwsh.exe"...), or ok=false
// for anything else (a server).
func localCommand(host string) (cmdline string, ok bool) {
	name, args, _ := strings.Cut(strings.TrimSpace(host), " ")
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	for _, s := range localShells {
		if s.name != name {
			continue
		}
		exe := s.exe
		if p, err := exec.LookPath(s.exe); err == nil {
			exe = p
		}
		cmd := quoteArg(exe)
		if args = strings.TrimSpace(args); args == "" {
			args = s.args
		}
		if args != "" {
			cmd += " " + args
		}
		return cmd, true
	}
	return "", false
}

// GetLocalShells lists the local shells installed on this PC.
func (a *App) GetLocalShells() []LocalShell {
	var list []LocalShell
	for _, s := range localShells {
		if _, err := exec.LookPath(s.exe); err == nil {
			list = append(list, LocalShell{Name: s.name, Label: s.label})
		}
	}
	return list
}

// localHome is the folder local shells start in.
func localHome() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Clean(home)
	}
	return ""
}
