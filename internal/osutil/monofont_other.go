//go:build !linux

package osutil

// readMonoFont: Windows and the Termux build use the page's own font list.
func readMonoFont() MonoFont { return MonoFont{} }
