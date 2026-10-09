//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// luaJob watches a script process's memory and kills it past the limit, as
// the Windows job object does. The process also dies with choboterm.
type luaJob struct {
	pid   int
	start string // the process's start time, so a reused pid is never killed
	limit uint64
	hit   atomic.Bool
	done  chan struct{}
	once  sync.Once
}

// hideWindow makes the script process end when choboterm does.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func newLuaJob(pid int, limitBytes uint64) (*luaJob, error) {
	j := &luaJob{pid: pid, start: procStart(pid), limit: limitBytes, done: make(chan struct{})}
	go j.watch()
	return j, nil
}

// procStart is field 22 of /proc/pid/stat (start time in clock ticks).
func procStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name (field 2) may hold spaces; count from its closing ')'.
	if i := bytes.LastIndexByte(b, ')'); i >= 0 {
		if f := bytes.Fields(b[i+1:]); len(f) > 19 {
			return string(f[19])
		}
	}
	return ""
}

// procRSS is the process's resident memory in bytes (0 if it is gone).
func procRSS(pid int) uint64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if v, ok := bytes.CutPrefix(line, []byte("VmRSS:")); ok {
			if f := bytes.Fields(v); len(f) > 0 {
				kb, _ := strconv.ParseUint(string(f[0]), 10, 64)
				return kb << 10
			}
		}
	}
	return 0
}

func (j *luaJob) alive() bool {
	return j.start != "" && procStart(j.pid) == j.start
}

func (j *luaJob) watch() {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-j.done:
			return
		case <-tick.C:
		}
		if !j.alive() {
			return
		}
		if procRSS(j.pid) >= j.limit {
			j.hit.Store(true)
			_ = syscall.Kill(j.pid, syscall.SIGKILL)
			return
		}
	}
}

// hitMemoryLimit reports whether the process was killed for its memory.
func (j *luaJob) hitMemoryLimit() bool { return j.hit.Load() }

// close stops watching and ends the process if it is still running.
func (j *luaJob) close() {
	j.once.Do(func() { close(j.done) })
	if j.alive() {
		_ = syscall.Kill(j.pid, syscall.SIGKILL)
	}
}
