//go:build !windows

package main

import (
	"os"
	"strings"
)

// systemKorean tells whether the locale is Korean.
func systemKorean() bool {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := os.Getenv(v); l != "" {
			return strings.HasPrefix(l, "ko")
		}
	}
	return false
}
