//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowClass names the main window's class so it can be found again.
const windowClass = "chobotermWindow"

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW      = user32.NewProc("FindWindowExW")
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
)

type winPoint struct{ X, Y int32 }

type winRect struct{ Left, Top, Right, Bottom int32 }

// winPlacement is WINDOWPLACEMENT.
type winPlacement struct {
	Length  uint32
	Flags   uint32
	ShowCmd uint32
	MinPos  winPoint
	MaxPos  winPoint
	Normal  winRect
}

const (
	swHide                = 0
	swShowMinimized       = 2
	swShowMaximized       = 3
	wpfRestoreToMaximized = 2
)

// mainWindow finds this process's main window.
func mainWindow() uintptr {
	cls, _ := windows.UTF16PtrFromString(windowClass)
	pid := uint32(os.Getpid())
	var h uintptr
	for {
		h, _, _ = procFindWindowExW.Call(0, h, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			return 0
		}
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(windows.HWND(h), &owner); err == nil && owner == pid {
			return h
		}
	}
}

// currentWindowState returns the window's normal (restored) bounds and
// whether it is maximized.
func currentWindowState() (WindowState, bool) {
	h := mainWindow()
	if h == 0 {
		return WindowState{}, false
	}
	wp := winPlacement{Length: uint32(unsafe.Sizeof(winPlacement{}))}
	if r, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return WindowState{}, false
	}
	return WindowState{
		Left:   int(wp.Normal.Left),
		Top:    int(wp.Normal.Top),
		Right:  int(wp.Normal.Right),
		Bottom: int(wp.Normal.Bottom),
		Maximized: wp.ShowCmd == swShowMaximized ||
			(wp.ShowCmd == swShowMinimized && wp.Flags&wpfRestoreToMaximized != 0),
	}, true
}

// restoreWindowBounds moves the (still hidden) window to its saved bounds.
// Windows moves it back on screen if the bounds are no longer visible,
// e.g. after a monitor was removed.
func restoreWindowBounds(s WindowState) bool {
	h := mainWindow()
	if h == 0 || !s.valid() {
		return false
	}
	wp := winPlacement{
		Length:  uint32(unsafe.Sizeof(winPlacement{})),
		ShowCmd: swHide,
		Normal:  winRect{int32(s.Left), int32(s.Top), int32(s.Right), int32(s.Bottom)},
	}
	r, _, _ := procSetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp)))
	return r != 0
}

var (
	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
)

const (
	gwlExStyle  = ^uintptr(19) // GWL_EXSTYLE (-20) as the unsigned argument
	wsExLayered = 0x00080000
	lwaAlpha    = 0x2
)

// setWindowAlpha makes the main window percent opaque. At 100 the window
// stops being a layered window, which draws a little faster.
func setWindowAlpha(percent int) {
	h := mainWindow()
	if h == 0 {
		return
	}
	ex, _, _ := procGetWindowLongPtrW.Call(h, gwlExStyle)
	if percent >= 100 {
		if ex&wsExLayered != 0 {
			procSetWindowLongPtrW.Call(h, gwlExStyle, ex&^wsExLayered)
		}
		return
	}
	if ex&wsExLayered == 0 {
		procSetWindowLongPtrW.Call(h, gwlExStyle, ex|wsExLayered)
	}
	procSetLayeredWindowAttributes.Call(h, 0, uintptr(percent*255/100), lwaAlpha)
}
