//go:build !windows

package osutil

import (
	"choboterm/internal/config"
)

const WindowClass = "chobotermWindow"

func CurrentWindowState() (config.WindowState, bool) { return config.WindowState{}, false }

func RestoreWindowBounds(config.WindowState) bool { return false }
