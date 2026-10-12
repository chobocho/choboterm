//go:build !windows && !linux

package secret

import "errors"

var ErrNoStore = errors.New("password storage is only supported on Windows")

// Passwords are only stored where the OS can encrypt them (DPAPI on Windows).
func Protect(plain []byte) ([]byte, error)    { return nil, ErrNoStore }
func Unprotect(cipher []byte) ([]byte, error) { return nil, ErrNoStore }
