//go:build windows

package main

import (
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procKeybdEvent          = user32.NewProc("keybd_event")
)

const (
	vkMenu          = 0x12
	keyeventfKeyUp  = 0x0002
	dialogClassName = "#32770" // common file dialogs and message boxes
)

// raiseDialogSoon brings the next dialog of this process to the front.
// It is for a dialog opened for a browser on this PC: the browser has the
// focus, and Windows only lets the foreground process take it, so the
// dialog would otherwise just flash in the taskbar behind the browser.
func raiseDialogSoon() {
	go func() {
		pid := uint32(os.Getpid())
		for i := 0; i < 60; i++ { // up to 3 seconds for the dialog to appear
			time.Sleep(50 * time.Millisecond)
			if h := findDialog(pid); h != 0 {
				// A key event lets this process change the foreground window
				// (the documented exceptions include "received the last input
				// event"); Alt alone types nothing.
				procKeybdEvent.Call(vkMenu, 0, 0, 0)
				procSetForegroundWindow.Call(h)
				procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
				return
			}
		}
	}()
}

// dialogSearch is the state of findDialog's EnumWindows callback. The
// callback is made once: Go can make only a limited number of callbacks
// and never frees them.
var dialogSearch struct {
	sync.Mutex
	pid   uint32
	found uintptr
}

var enumDialogs = syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
	if v, _, _ := procIsWindowVisible.Call(h); v == 0 {
		return 1
	}
	var owner uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(h), &owner); err != nil || owner != dialogSearch.pid {
		return 1
	}
	var cls [64]uint16
	n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&cls[0])), uintptr(len(cls)))
	if windows.UTF16ToString(cls[:n]) == dialogClassName {
		dialogSearch.found = h
		return 0 // stop
	}
	return 1
})

// findDialog returns a visible dialog window of process pid, or 0.
func findDialog(pid uint32) uintptr {
	dialogSearch.Lock()
	defer dialogSearch.Unlock()
	dialogSearch.pid, dialogSearch.found = pid, 0
	procEnumWindows.Call(enumDialogs, 0) // calls back on this thread before returning
	return dialogSearch.found
}
