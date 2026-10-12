//go:build termux

package main

import "errors"

var errNoSecretStore = errors.New("password storage is not supported in the Termux build")

func protectSecret([]byte) ([]byte, error)   { return nil, errNoSecretStore }
func unprotectSecret([]byte) ([]byte, error) { return nil, errNoSecretStore }
