//go:build !windows

package osutil

// RaiseDialogSoon does nothing outside Windows: window managers give a new
// dialog the focus.
func RaiseDialogSoon() {}
