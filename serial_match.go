//go:build !termux

package main

import "strings"

const serialSupported = true

// isSerial reports whether the Host field names a serial port.
func isSerial(host string) bool {
	name, _, _ := strings.Cut(strings.TrimSpace(host), " ")
	return serialName.MatchString(name)
}
