//go:build !windows

package main

import "os/exec"

// Without job objects the script process has no memory limit; it is still
// stopped with the tab and killed when it doesn't stop.
type luaJob struct{}

func hideWindow(cmd *exec.Cmd) {}

func newLuaJob(pid int, limitBytes uint64) (*luaJob, error) { return &luaJob{}, nil }

func (j *luaJob) hitMemoryLimit() bool { return false }

func (j *luaJob) close() {}
