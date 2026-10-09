//go:build linux

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectLocalShellLinux(t *testing.T) {
	orig := historyFile
	hist := filepath.Join(t.TempDir(), "hosts.json")
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = orig }()
	useTempSettings(t)

	a := NewApp()
	o := newTabOutput(a)
	proto, err := a.Connect(1, ConnectRequest{Host: "bash", Encoding: "EUC-KR", Cols: 80, Rows: 24})
	if err != nil || proto != "local" {
		t.Fatalf("connect: %q %v", proto, err)
	}
	a.Resize(1, 101, 33)
	a.Send(1, "echo 한글-$((6*7)) $TERM; stty size\r") // UTF-8 even though EUC-KR was chosen
	o.waitText(t, 1, "한글-42 xterm-256color")
	o.waitText(t, 1, "33 101")
	a.Send(1, "exit\r")
	o.waitFor(t, 1, "end after exit", func() bool { return o.isClosed(1) })
	o.mu.Lock()
	msg := o.closed[1]
	o.mu.Unlock()
	if msg != "연결이 종료되었습니다" {
		t.Fatalf("closed: %q", msg)
	}
	if h := loadHistory(); len(h) != 1 || h[0].Host != "bash" || h[0].Port != 0 {
		t.Fatalf("history %+v", h)
	}

	// Closing the tab ends a shell that is still running.
	if _, err := a.Connect(2, ConnectRequest{Host: "sh", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	a.Send(2, "echo M-$((6*7))\r")
	o.waitText(t, 2, "M-42")
	a.Disconnect(2)
}

func TestLocalCommandLinux(t *testing.T) {
	for host, want := range map[string]string{
		"bash":            "/bash",
		"bash -l":         "/bash -l",
		"sh":              "/sh",
		"busan":           "",
		"ssh -p 22 me@sh": "",
	} {
		got, ok := localCommand(host)
		if want == "" {
			if ok {
				t.Errorf("%q: got %q, want a server", host, got)
			}
			continue
		}
		if !ok || !strings.HasSuffix(got, want) {
			t.Errorf("%q: got %q, want ...%q", host, got, want)
		}
	}
	if got := commandLine("/usr/bin/docker", []string{"exec", "-it", "web", "sh", "-c", dockerShell}); got != "/usr/bin/docker exec -it web sh -c '"+dockerShell+"'" {
		t.Errorf("commandLine: %q", got)
	}
}
