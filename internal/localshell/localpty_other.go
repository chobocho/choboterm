//go:build !windows && !linux

package localshell

import (
	"errors"

	"choboterm/internal/terminal"
)

func Start(cmdline, dir string, cols, rows int) (terminal.Session, error) {
	return nil, errors.New("로컬 셸은 Windows에서만 쓸 수 있습니다")
}
