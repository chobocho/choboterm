//go:build windows

package main

import "strings"

// localShells are the Host field names that start a local program, in list order.
var localShells = []struct {
	name, label, exe, args string
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

// commandLine is exe with args as a command line for startPty.
func commandLine(exe string, args []string) string {
	cmd := winQuote(exe)
	for _, a := range args {
		cmd += " " + winQuote(a)
	}
	return cmd
}
