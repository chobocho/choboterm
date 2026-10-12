//go:build termux

package secret

import "errors"

var ErrNoStore = errors.New("password storage is not supported in the Termux build")

func Protect([]byte) ([]byte, error)   { return nil, ErrNoStore }
func Unprotect([]byte) ([]byte, error) { return nil, ErrNoStore }
