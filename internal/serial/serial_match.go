//go:build !termux

package serial

import "strings"

const Supported = true

// IsPort reports whether the Host field names a serial port.
func IsPort(host string) bool {
	name, _, _ := strings.Cut(strings.TrimSpace(host), " ")
	return serialName.MatchString(name)
}
