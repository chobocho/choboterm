//go:build !windows && !linux

package main

import "errors"

var errNoSecretStore = errors.New("password storage is only supported on Windows")

// Passwords are only stored where the OS can encrypt them (DPAPI on Windows).
func protectSecret(plain []byte) ([]byte, error)    { return nil, errNoSecretStore }
func unprotectSecret(cipher []byte) ([]byte, error) { return nil, errNoSecretStore }
