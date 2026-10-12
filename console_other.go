//go:build !windows

package main

// attachConsole is only needed on Windows (a GUI program has no console).
func attachConsole() {}
