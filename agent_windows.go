//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformAgentSources: the OpenSSH for Windows agent service and PuTTY's Pageant.
func platformAgentSources() []agentSource {
	return []agentSource{
		{"OpenSSH", func() (io.ReadWriteCloser, error) {
			return os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
		}},
		{"Pageant", dialPageant},
	}
}

var (
	procSendMessageW = user32.NewProc("SendMessageW")
	pageantSeq       atomic.Uint32
)

const (
	wmCopyData        = 0x004A
	pageantCopyDataID = 0x804e50ba // AGENT_COPYDATA_ID in PuTTY
	pageantMaxMsg     = 8192       // AGENT_MAX_MSGLEN in PuTTY
)

type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

// pageantConn speaks the SSH agent protocol with Pageant: each request is
// put in shared memory and handed over with WM_COPYDATA. Replies are read
// like from a socket: Read waits for the next one until the conn is closed.
type pageantConn struct {
	hwnd    windows.HWND
	replies chan []byte
	done    chan struct{}
	close   sync.Once
	pending []byte
}

func dialPageant() (io.ReadWriteCloser, error) {
	name, _ := windows.UTF16PtrFromString("Pageant")
	hwnd, _, _ := procFindWindowExW.Call(0, 0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		return nil, errors.New("Pageant가 실행 중이 아닙니다")
	}
	return &pageantConn{hwnd: windows.HWND(hwnd), replies: make(chan []byte, 1), done: make(chan struct{})}, nil
}

// Write sends one whole request (the agent client writes each in one call).
func (c *pageantConn) Write(req []byte) (int, error) {
	if len(req) < 4 || int(binary.BigEndian.Uint32(req))+4 != len(req) || len(req) > pageantMaxMsg {
		return 0, errors.New("pageant: bad request")
	}
	mapName := fmt.Sprintf("PageantRequest%08x%08x", os.Getpid(), pageantSeq.Add(1))
	nameBytes := append([]byte(mapName), 0) // Pageant takes the name as ANSI
	name16, _ := windows.UTF16PtrFromString(mapName)
	h, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, pageantMaxMsg, name16)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, 0)
	if err != nil {
		return 0, err
	}
	defer windows.UnmapViewOfFile(addr)
	// addr is mapped memory, outside the Go heap.
	view := unsafe.Slice((*byte)(unsafe.Add(nil, addr)), pageantMaxMsg)
	copy(view, req)

	cds := copyDataStruct{dwData: pageantCopyDataID, cbData: uint32(len(nameBytes)), lpData: uintptr(unsafe.Pointer(&nameBytes[0]))}
	ret, _, _ := procSendMessageW.Call(uintptr(c.hwnd), wmCopyData, 0, uintptr(unsafe.Pointer(&cds)))
	if ret == 0 {
		return 0, errors.New("Pageant가 요청을 거부했습니다")
	}
	n := binary.BigEndian.Uint32(view)
	if n+4 > pageantMaxMsg {
		return 0, errors.New("pageant: reply too long")
	}
	select {
	case c.replies <- append([]byte(nil), view[:4+n]...):
	case <-c.done:
		return 0, io.ErrClosedPipe
	}
	return len(req), nil
}

func (c *pageantConn) Read(p []byte) (int, error) {
	if len(c.pending) == 0 {
		select {
		case c.pending = <-c.replies:
		case <-c.done:
			return 0, io.EOF
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *pageantConn) Close() error {
	c.close.Do(func() { close(c.done) })
	return nil
}
