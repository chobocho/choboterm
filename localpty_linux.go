//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// ptySession is a local program (a shell, docker exec) in a pseudo terminal.
type ptySession struct {
	cmd    *exec.Cmd
	tty    *os.File
	closed sync.Once
}

// startPty runs cmdline (shell syntax, see commandLine) in a new pseudo
// terminal of cols x rows, in dir.
func startPty(cmdline, dir string, cols, rows int) (Session, error) {
	cmd := exec.Command("/bin/sh", "-c", "exec "+cmdline)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	s := &ptySession{cmd: cmd, tty: tty}
	go func() { _ = cmd.Wait() }() // reap it; the reader sees the end
	return s, nil
}

// Read returns io.EOF once the program has exited (or the tab closed it).
func (s *ptySession) Read(p []byte) (int, error) {
	n, err := s.tty.Read(p)
	if err != nil {
		// Linux reports the end of the terminal's other side as EIO.
		if errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
			err = io.EOF
		}
	}
	return n, err
}

func (s *ptySession) Write(p []byte) (int, error) { return s.tty.Write(p) }

func (s *ptySession) Resize(cols, rows int) error {
	return pty.Setsize(s.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close ends the program (SIGHUP, as closing a terminal window does).
func (s *ptySession) Close() error {
	s.closed.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Signal(syscall.SIGHUP)
		}
		_ = s.tty.Close()
	})
	return nil
}
