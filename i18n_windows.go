package main

import "golang.org/x/sys/windows"

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// systemKorean tells whether Windows shows its own screens in Korean.
func systemKorean() bool {
	id, _, _ := procGetUserDefaultUILanguage.Call()
	return id&0x3ff == 0x12 // LANG_KOREAN
}
