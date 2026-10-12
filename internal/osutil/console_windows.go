//go:build windows

package osutil

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// AttachConsole connects stdout and stderr to the console choboterm was
// started from: the exe is a GUI program (no console of its own), but
// --headless and --help print there.
func AttachConsole() {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return // started from Explorer: nowhere to print
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}
