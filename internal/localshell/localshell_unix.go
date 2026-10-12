//go:build !windows

package localshell

import "strings"

// Shells are the Host field names that start a local program, in list order.
var Shells = []struct {
	Name, Label, Exe, args string
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
	return ShQuote(s)
}

// CommandLine is exe with args as a command line for startPty.
func CommandLine(exe string, args []string) string {
	cmd := quoteArg(exe)
	for _, a := range args {
		cmd += " " + quoteArg(a)
	}
	return cmd
}
