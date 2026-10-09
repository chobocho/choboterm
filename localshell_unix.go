//go:build !windows

package main

import "strings"

// localShells are the Host field names that start a local program, in list order.
var localShells = []struct {
	name, label, exe, args string
}{
	{"bash", "Bash", "bash", ""},
	{"zsh", "Zsh", "zsh", ""},
	{"fish", "Fish", "fish", ""},
	{"sh", "sh", "sh", ""},
}

// quoteArg quotes a program path for the shell that runs the command line.
func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return shQuote(s)
}

// commandLine is exe with args as a command line for startPty.
func commandLine(exe string, args []string) string {
	cmd := quoteArg(exe)
	for _, a := range args {
		cmd += " " + quoteArg(a)
	}
	return cmd
}
