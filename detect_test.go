package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// greetServer accepts one connection and writes greeting (in two pieces, to
// check that a split greeting is still recognized), then echoes.
func greetServer(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if greeting != "" {
			half := len(greeting) / 2
			c.Write([]byte(greeting[:half]))
			time.Sleep(50 * time.Millisecond)
			c.Write([]byte(greeting[half:]))
		}
		io.Copy(c, c)
	}()
	return ln.Addr().String()
}

func TestDialDetect(t *testing.T) {
	saved := detectWait
	detectWait = 300 * time.Millisecond
	defer func() { detectWait = saved }()

	for _, c := range []struct {
		greeting, want string
	}{
		{"SSH-2.0-OpenSSH_10.5\r\n", "ssh"},
		{"220 ProFTPD Server ready.\r\n", "ftp"},
		{"220-Welcome\r\n220 ready\r\n", "ftp"},
		{"\xff\xfd\x18\xff\xfd\x20", "telnet"},
		{"Welcome to the BBS\r\nlogin: ", "telnet"},
		{"", "telnet"}, // silent server
	} {
		conn, proto, err := dialDetect(greetServer(t, c.greeting))
		if err != nil {
			t.Fatal(err)
		}
		if proto != c.want {
			t.Errorf("%q: detected %q, want %q", c.greeting, proto, c.want)
		}
		// The greeting must still be readable afterwards (replayed), followed by live data.
		conn.Write([]byte("ping"))
		want := c.greeting + "ping"
		got := make([]byte, len(want))
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != want {
			t.Errorf("%q: replay = %q, %v", c.greeting, got, err)
		}
		conn.Close()
	}
}

// TestLiveSSH connects to a real SSH server on any port through App.Connect:
//
//	CHOBOTERM_SSH_TEST=host:port CHOBOTERM_SSH_USER=user CHOBOTERM_SSH_PASS=secret go test -run LiveSSH -v
//
// The host key is accepted into a temporary known_hosts, not ~/.ssh.
func TestLiveSSH(t *testing.T) {
	addr := os.Getenv("CHOBOTERM_SSH_TEST")
	if addr == "" {
		t.Skip("CHOBOTERM_SSH_TEST not set")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	orig := historyFile
	historyFile = func() (string, error) { return home + "/hosts.json", nil }
	defer func() { historyFile = orig }()

	var (
		mu     sync.Mutex
		screen bytes.Buffer
	)
	a := NewApp()
	a.hooks.confirmKey = func(string, string) bool { return true }
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "term:data" {
			b, _ := base64.StdEncoding.DecodeString(data[1].(string))
			mu.Lock()
			screen.Write(b)
			mu.Unlock()
		}
	}
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := strings.Contains(screen.String(), want)
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("timed out waiting for %q; screen:\n%s", want, screen.String())
	}

	var port int
	for _, ch := range portStr {
		port = port*10 + int(ch-'0')
	}
	proto, err := a.Connect(1, ConnectRequest{
		Host: host, Port: port,
		Login: os.Getenv("CHOBOTERM_SSH_USER"), Pass: os.Getenv("CHOBOTERM_SSH_PASS"),
		Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseTab(1)
	if proto != "ssh" {
		t.Fatalf("protocol = %q, want ssh", proto)
	}

	a.Send(1, "echo choboterm-$((6*7))\r")
	waitFor("choboterm-42")
	a.Resize(1, 120, 40)
	a.Send(1, "stty size\r")
	waitFor("40 120")

	res, err := a.FileOpen(1)
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.FileList(1, res.Home)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("file access via %s, %s has %d entries", res.Protocol, res.Home, len(list))

	kh, _ := os.ReadFile(home + "/.ssh/known_hosts")
	if !strings.Contains(string(kh), "["+host+"]:"+portStr) {
		t.Fatalf("known_hosts entry missing for non-standard port:\n%s", kh)
	}
}
