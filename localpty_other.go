//go:build !windows && !linux

package main

import (
	"errors"

	"choboterm/internal/terminal"
)

func startPty(cmdline, dir string, cols, rows int) (terminal.Session, error) {
	return nil, errors.New("로컬 셸은 Windows에서만 쓸 수 있습니다")
}
