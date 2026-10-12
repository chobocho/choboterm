//go:build !windows

package main

// raiseDialogSoon does nothing outside Windows: window managers give a new
// dialog the focus.
func raiseDialogSoon() {}
