//go:build linux

package main

import (
	"io"
	"testing"
	"time"

	"github.com/creack/pty"
)

// A pseudo terminal stands in for a serial port: termios works on it the same.
func TestSerialSessionPty(t *testing.T) {
	dev, tty, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer dev.Close()
	name := tty.Name()
	tty.Close()

	s, err := openSerial(serialConfig{Name: name, Baud: 9600, DataBits: 8, Parity: 'N', StopBits: 1, Flow: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openSerial(serialConfig{Name: name, Baud: 9600, DataBits: 8, Parity: 'N', StopBits: 1, Flow: "none"}); err == nil {
		t.Error("a second open of the same port succeeded")
	}
	_, _ = s.Write([]byte("show ver\r"))
	b := make([]byte, 64)
	n, _ := dev.Read(b)
	if string(b[:n]) != "show ver\r" { // raw mode: no echo, \r not turned into \n
		t.Errorf("device got %q", b[:n])
	}
	_, _ = dev.Write([]byte("Router>"))
	n, _ = s.Read(b)
	if string(b[:n]) != "Router>" {
		t.Errorf("read %q", b[:n])
	}
	if err := s.sendBreak(); err != nil {
		t.Errorf("sendBreak: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := s.Read(b)
		errc <- err
	}()
	time.Sleep(100 * time.Millisecond)
	_ = s.Close()
	select {
	case err := <-errc:
		if err != io.EOF {
			t.Errorf("read after Close = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close didn't end the waiting read")
	}
}
