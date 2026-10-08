//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// luaJob holds a script process in a Windows job object: the OS ends the
// process when it uses more memory than allowed, and when choboterm exits
// (the job handle closes with it), so a script never outlives the app.
type luaJob struct {
	h     windows.Handle
	limit uintptr
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
	return &luaJob{h: h, limit: uintptr(limitBytes)}, nil
}

// hitMemoryLimit reports whether the process came close to the memory limit,
// which is the likely reason it ended without saying why.
func (j *luaJob) hitMemoryLimit() bool {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(j.h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return false
	}
	return info.PeakProcessMemoryUsed >= j.limit/10*9
}

func (j *luaJob) close() {
	windows.CloseHandle(j.h) // ends the process too, if it is still running
}
