//go:build !windows

package main

import "errors"

// puttySessions: PuTTY keeps its sessions in the Windows registry.
func puttySessions() ([]SavedSession, error) {
	return nil, errors.New("PuTTY 세션 가져오기는 Windows에서만 됩니다")
}
