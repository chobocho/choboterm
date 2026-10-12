package main

import (
	"runtime"
	"strings"
	"testing"

	"choboterm/internal/serial"
	"choboterm/internal/terminal"
)

func TestConnectSerialMissingPort(t *testing.T) {
	if !serial.Supported {
		t.Skip("serial ports are disabled in this build")
	}
	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	name := "COM250"
	if runtime.GOOS == "linux" {
		name = "/dev/ttyUSB250"
	}
	_, err := a.Connect(1, terminal.ConnectRequest{Host: name, Port: 9600})
	if err == nil || !strings.Contains(err.Error(), "포트가 없습니다") {
		t.Fatalf("Connect(%s) error = %v, want a missing port", name, err)
	}
}
