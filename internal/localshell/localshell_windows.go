//go:build windows

package localshell

import "strings"

// Shells are the Host field names that start a local program, in list order.
var Shells = []struct {
	Name, Label, Exe, args string
}{
	{"cmd", "명령 프롬프트 (cmd)", "cmd.exe", ""},
	{"powershell", "Windows PowerShell", "powershell.exe", "-NoLogo"},
	{"pwsh", "PowerShell 7 (pwsh)", "pwsh.exe", "-NoLogo"},
	{"wsl", "WSL (Linux)", "wsl.exe", "--cd ~"},
}

// quoteArg quotes a program path for a Windows command line when it has spaces.
func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

// CommandLine is exe with args as a command line for startPty.
func CommandLine(exe string, args []string) string {
	cmd := WinQuote(exe)
	for _, a := range args {
		cmd += " " + WinQuote(a)
	}
	return cmd
}
