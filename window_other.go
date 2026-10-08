//go:build !windows

package main

const windowClass = "chobotermWindow"

func currentWindowState() (WindowState, bool) { return WindowState{}, false }

func restoreWindowBounds(WindowState) bool { return false }

func setWindowAlpha(int) {}
