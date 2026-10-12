//go:build !windows

package osutil

// AttachConsole is only needed on Windows (a GUI program has no console).
func AttachConsole() {}
