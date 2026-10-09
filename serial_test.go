package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseSerial(t *testing.T) {
	cases := []struct {
		host string
		port int
		want string // serialConfig.String(), or "error"
	}{
		{"COM3", 0, "COM3 115200 8N1"},
		{"com3", 9600, "COM3 9600 8N1"},
		{"COM3", 22, "COM3 115200 8N1"}, // a server's port left in the field
		{"COM3 9600", 115200, "COM3 9600 8N1"},
		{"COM12 19200 7e1 rtscts", 0, "COM12 19200 7E1 rtscts"},
		{"/dev/ttyUSB0", 57600, "/dev/ttyUSB0 57600 8N1"},
		{"/dev/ttyACM0 8O2 xonxoff", 0, "/dev/ttyACM0 115200 8O2 xonxoff"},
		{"COM3 9600 9N1", 0, "error"},
		{"COM3 fast", 0, "error"},
		{"COM3 10", 0, "error"},
		{"COM0", 0, "error"},
	}
	for _, c := range cases {
		got, err := parseSerial(c.host, c.port)
		s := got.String()
		if err != nil {
			s = "error"
		}
		if s != c.want {
			t.Errorf("parseSerial(%q, %d) = %q (%v), want %q", c.host, c.port, s, err, c.want)
		}
	}
}

func TestIsSerial(t *testing.T) {
	for host, want := range map[string]bool{
		"COM1": true, "com3 9600": true, " /dev/ttyUSB0 ": true,
		"COM": false, "computer": false, "com3.example.com": false, "cmd": false, "192.168.0.1": false,
	} {
		if got := isSerial(host); got != want {
			t.Errorf("isSerial(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestConnectSerialMissingPort(t *testing.T) {
	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	name := "COM250"
	if runtime.GOOS == "linux" {
		name = "/dev/ttyUSB250"
	}
	_, err := a.Connect(1, ConnectRequest{Host: name, Port: 9600})
	if err == nil || !strings.Contains(err.Error(), "포트가 없습니다") {
		t.Fatalf("Connect(%s) error = %v, want a missing port", name, err)
	}
}
