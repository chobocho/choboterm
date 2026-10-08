//go:build windows

package main

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptySession runs a local program in a Windows pseudo console (ConPTY), so
// cmd, PowerShell and WSL work like a remote shell in a tab.
type ptySession struct {
	pty windows.Handle
	in  *os.File // our end of the console's input
	out *os.File // our end of the console's output

	closePty sync.Once
	closed   sync.Once
}

// startPty starts cmdline (with dir as its folder) in a cols x rows console.
func startPty(cmdline, dir string, cols, rows int) (Session, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, err
	}
	var hpc windows.Handle
	err := windows.CreatePseudoConsole(ptySize(cols, rows), inR, outW, 0, &hpc)
	// The console keeps its own copies of its ends.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if err != nil {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("의사 콘솔을 만들 수 없습니다 (Windows 10 1809 이상 필요): %w", err)
	}
	s := &ptySession{pty: hpc, in: os.NewFile(uintptr(inW), "pty-in"), out: os.NewFile(uintptr(outR), "pty-out")}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		s.Close()
		return nil, err
	}
	defer attrs.Delete()
	// The attribute value is the console handle itself.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Add(nil, uintptr(hpc)), unsafe.Sizeof(hpc)); err != nil {
		s.Close()
		return nil, err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// No standard handles: the program must use the pseudo console, not ours.
	si.Flags = windows.STARTF_USESTDHANDLES

	cmd16, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		s.Close()
		return nil, err
	}
	var dir16 *uint16
	if dir != "" {
		dir16, _ = windows.UTF16PtrFromString(dir)
	}
	var pi windows.ProcessInformation
	err = windows.CreateProcess(nil, cmd16, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, nil, dir16, &si.StartupInfo, &pi)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("%s 실행 실패: %w", cmdline, err)
	}
	windows.CloseHandle(pi.Thread)

	// The console's output only ends when the console is closed, so close it
	// when the program exits; Read then sees the end.
	go func() {
		_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		debugf("local program exited: %s", cmdline)
		s.closeConsole()
		windows.CloseHandle(pi.Process)
	}()
	return s, nil
}

func ptySize(cols, rows int) windows.Coord {
	return windows.Coord{X: int16(max(cols, 1)), Y: int16(max(rows, 1))}
}

func (s *ptySession) closeConsole() {
	s.closePty.Do(func() {
		// Ends the attached programs (CTRL_CLOSE_EVENT) and the output pipe.
		windows.ClosePseudoConsole(s.pty)
	})
}

// Read returns io.EOF once the program has exited (or the tab closed it).
func (s *ptySession) Read(p []byte) (int, error) {
	n, err := s.out.Read(p)
	if err != nil {
		_ = s.out.Close()
	}
	return n, err
}

func (s *ptySession) Write(p []byte) (int, error) { return s.in.Write(p) }

func (s *ptySession) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(s.pty, ptySize(cols, rows))
}

// Close ends the program. Closing the console from another goroutine lets
// the reader drain the last output while it shuts down.
func (s *ptySession) Close() error {
	s.closed.Do(func() {
		go func() {
			s.closeConsole()
			_ = s.in.Close()
		}()
	})
	return nil
}
