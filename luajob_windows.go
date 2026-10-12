//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// luaJob holds a script process in a Windows job object, so a script never
// outlives the app (the process ends when the job handle closes with it).
//
// The job's memory limit only makes allocations past it fail; what the
// process then does is up to it. Usually Go exits ("out of memory"), but it
// can also stay stuck at the limit (seen on GitHub's Windows runners), so
// watch ends the job once the process has reached the limit, like the Linux
// watcher does.
type luaJob struct {
	mu    sync.Mutex // guards h against close while watch queries it
	h     windows.Handle
	limit uintptr
	done  chan struct{}
	once  sync.Once
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func newLuaJob(pid int, limitBytes uint64) (*luaJob, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
				windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
				windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION,
		},
		ProcessMemoryLimit: uintptr(limitBytes),
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	defer windows.CloseHandle(p)
	if err := windows.AssignProcessToJobObject(h, p); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	j := &luaJob{h: h, limit: uintptr(limitBytes), done: make(chan struct{})}
	go j.watch()
	return j, nil
}

// limitSlack is how close to the limit counts as reaching it: allocations
// fail in chunks, so the peak may stop a little short of the limit.
const limitSlack = 1 << 20

// watch ends the job when the process has used all the memory it may.
func (j *luaJob) watch() {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-j.done:
			return
		case <-tick.C:
		}
		j.mu.Lock()
		if j.h == 0 {
			j.mu.Unlock()
			return
		}
		if info, err := j.info(); err == nil && info.PeakProcessMemoryUsed+limitSlack >= j.limit {
			_ = windows.TerminateJobObject(j.h, 1)
			j.mu.Unlock()
			return
		}
		j.mu.Unlock()
	}
}

// info reads the job's limits and memory use (j.mu held).
func (j *luaJob) info() (windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err := windows.QueryInformationJobObject(j.h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	return info, err
}

// hitMemoryLimit reports whether the process came close to the memory limit,
// which is the likely reason it ended without saying why.
func (j *luaJob) hitMemoryLimit() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return false
	}
	info, err := j.info()
	return err == nil && info.PeakProcessMemoryUsed >= j.limit/10*9
}

// memoryReport describes the process's memory use against the limit (for
// diagnosing a script that didn't end).
func (j *luaJob) memoryReport() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return "job closed"
	}
	info, err := j.info()
	if err != nil {
		return "job query failed: " + err.Error()
	}
	return fmt.Sprintf("peak process memory %dMB, job peak %dMB, limit %dMB, flags %#x",
		info.PeakProcessMemoryUsed>>20, info.PeakJobMemoryUsed>>20, j.limit>>20, info.BasicLimitInformation.LimitFlags)
}

func (j *luaJob) close() {
	j.once.Do(func() {
		close(j.done)
		j.mu.Lock()
		windows.CloseHandle(j.h) // ends the process too, if it is still running
		j.h = 0
		j.mu.Unlock()
	})
}
