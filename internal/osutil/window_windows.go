//go:build windows

package osutil

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"choboterm/internal/config"
)

// WindowClass names the main window's class so it can be found again.
const WindowClass = "chobotermWindow"

var (
	User32                 = windows.NewLazySystemDLL("user32.dll")
	ProcFindWindowExW      = User32.NewProc("FindWindowExW")
	procGetWindowPlacement = User32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = User32.NewProc("SetWindowPlacement")
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
	cls, _ := windows.UTF16PtrFromString(WindowClass)
	pid := uint32(os.Getpid())
	var h uintptr
	for {
		h, _, _ = ProcFindWindowExW.Call(0, h, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			return 0
		}
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(windows.HWND(h), &owner); err == nil && owner == pid {
			return h
		}
	}
}

// CurrentWindowState returns the window's normal (restored) bounds and
// whether it is maximized.
func CurrentWindowState() (config.WindowState, bool) {
	h := mainWindow()
	if h == 0 {
		return config.WindowState{}, false
	}
	wp := winPlacement{Length: uint32(unsafe.Sizeof(winPlacement{}))}
	if r, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return config.WindowState{}, false
	}
	return config.WindowState{
		Left:   int(wp.Normal.Left),
		Top:    int(wp.Normal.Top),
		Right:  int(wp.Normal.Right),
		Bottom: int(wp.Normal.Bottom),
		Maximized: wp.ShowCmd == swShowMaximized ||
			(wp.ShowCmd == swShowMinimized && wp.Flags&wpfRestoreToMaximized != 0),
	}, true
}

// RestoreWindowBounds moves the (still hidden) window to its saved bounds.
// Windows moves it back on screen if the bounds are no longer visible,
// e.g. after a monitor was removed.
func RestoreWindowBounds(s config.WindowState) bool {
	h := mainWindow()
	if h == 0 || !s.Valid() {
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
