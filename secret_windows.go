//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errNoSecretStore: DPAPI is always there on Windows.
var errNoSecretStore = errors.New("비밀번호를 저장할 수 없습니다")

// protectSecret encrypts data with DPAPI for the current Windows user.
func protectSecret(plain []byte) ([]byte, error) {
	return dpapi(plain, true)
}

// unprotectSecret decrypts data produced by protectSecret.
func unprotectSecret(cipher []byte) ([]byte, error) {
	return dpapi(cipher, false)
}

func dpapi(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
