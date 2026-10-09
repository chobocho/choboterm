//go:build windows

package main

import (
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A named pipe stands in for a COM port: both are overlapped handles that
// os.File runs through the I/O completion port.
func pipePort(t *testing.T) (*serialSession, *os.File) {
	t.Helper()
	name, _ := windows.UTF16PtrFromString(`\\.\pipe\choboterm-serial-` + t.Name())
	srv, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED, 0, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	other := os.NewFile(uintptr(srv), "server")
	t.Cleanup(func() { other.Close() })
	return &serialSession{f: os.NewFile(uintptr(h), "COM")}, other
}

func TestSerialSessionReadWrite(t *testing.T) {
	s, dev := pipePort(t)
	defer s.Close()
	got := make(chan string, 1)
	go func() {
		b := make([]byte, 64)
		n, _ := s.Read(b)
		got <- string(b[:n])
	}()
	// Typing while a read waits for the device must not block.
	done := make(chan struct{})
	go func() {
		_, _ = s.Write([]byte("show ver\r"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("write blocked behind a waiting read")
	}
	b := make([]byte, 64)
	n, _ := dev.Read(b)
	if string(b[:n]) != "show ver\r" {
		t.Fatalf("device got %q", b[:n])
	}
	_, _ = dev.Write([]byte("Router>"))
	if g := <-got; g != "Router>" {
		t.Fatalf("read %q, want Router>", g)
	}
}

func TestSerialSessionCloseEndsRead(t *testing.T) {
	s, _ := pipePort(t)
	errc := make(chan error, 1)
	go func() {
		_, err := s.Read(make([]byte, 16))
		errc <- err
	}()
	time.Sleep(100 * time.Millisecond)
	_ = s.Close()
	select {
	case err := <-errc:
		if err != io.EOF {
			t.Fatalf("read after Close = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close didn't end the waiting read")
	}
}

func TestSerialSessionDeviceGone(t *testing.T) {
	s, dev := pipePort(t)
	defer s.Close()
	errc := make(chan error, 1)
	go func() {
		_, err := s.Read(make([]byte, 16))
		errc <- err
	}()
	time.Sleep(100 * time.Millisecond)
	_ = dev.Close() // the other end goes away, as an unplugged adapter does
	select {
	case err := <-errc:
		if err != errSerialGone {
			t.Fatalf("read = %v, want errSerialGone", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read didn't end")
	}
}

func TestListSerialPorts(t *testing.T) {
	for _, p := range listSerialPorts() {
		if !isSerial(p.Name) {
			t.Errorf("listed %q, which the Host field doesn't take as a serial port", p.Name)
		}
	}
}
