//go:build !windows && !linux

package luahost

import "os/exec"

// Without job objects the script process has no memory limit; it is still
// stopped with the tab and killed when it doesn't stop.
type Job struct{}

func HideWindow(cmd *exec.Cmd) {}

func NewJob(pid int, limitBytes uint64) (*Job, error) { return &Job{}, nil }

func (j *Job) HitMemoryLimit() bool { return false }

func (j *Job) Close() {}

func (j *Job) MemoryReport() string { return "no memory limit on this OS" }
