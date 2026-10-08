//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// readUntil reads s until its output contains want or ends.
func readUntil(t *testing.T, s Session, want string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				done <- err
				return
			}
			if want != "" && strings.Contains(out.String(), want) {
				done <- nil
				return
			}
		}
	}()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
		return "", nil
	}
}

func TestPtyRunsProgramToEnd(t *testing.T) {
	s, err := startPty("cmd.exe /c echo hello-pty", "", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	out, err := readUntil(t, s, "")
	if err != io.EOF || !strings.Contains(out, "hello-pty") {
		t.Fatalf("out %q, err %v", out, err)
	}
}

func TestPtyInteractiveAndClose(t *testing.T) {
	s, err := startPty("cmd.exe", "", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("echo typed-%OS%\r")); err != nil {
		t.Fatal(err)
	}
	if out, err := readUntil(t, s, "typed-Windows_NT"); err != nil {
		t.Fatalf("out %q, err %v", out, err)
	}
	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Closing the tab ends the shell and the output.
	s.Close()
	if _, err := readUntil(t, s, ""); err != io.EOF {
		t.Fatalf("after close: %v", err)
	}
}

func TestConnectLocalShell(t *testing.T) {
	orig := historyFile
	hist := filepath.Join(t.TempDir(), "hosts.json")
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = orig }()
	useTempSettings(t)

	var (
		mu     sync.Mutex
		screen bytes.Buffer
		closed = make(chan string, 1)
	)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(data[1].(string))
			mu.Lock()
			screen.Write(b)
			mu.Unlock()
		case "term:closed":
			closed <- data[1].(string)
		}
	}
	proto, err := a.Connect(1, ConnectRequest{Host: "cmd", Encoding: "EUC-KR", Cols: 80, Rows: 24})
	if err != nil || proto != "local" {
		t.Fatalf("connect: %q %v", proto, err)
	}
	a.Send(1, "echo 한글-ok\r") // UTF-8 even though EUC-KR was chosen
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		got := screen.String()
		mu.Unlock()
		if strings.Count(got, "한글-ok") >= 2 { // the echoed command and its output
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen %q", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.Send(1, "exit\r")
	select {
	case msg := <-closed:
		if msg != "연결이 종료되었습니다" {
			t.Fatalf("closed: %q", msg)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no term:closed after exit")
	}
	if h := loadHistory(); len(h) != 1 || h[0].Host != "cmd" || h[0].Port != 0 {
		t.Fatalf("history %+v", h)
	}
}

func TestLocalCommand(t *testing.T) {
	for host, want := range map[string]string{
		"cmd":              "cmd.exe",
		"CMD.EXE":          "cmd.exe",
		"powershell":       "powershell.exe -NoLogo",
		"wsl -d Ubuntu":    "wsl.exe -d Ubuntu",
		"busan":            "",
		"ssh -p 22 me@cmd": "",
	} {
		got, ok := localCommand(host)
		if want == "" {
			if ok {
				t.Errorf("%q: got %q, want a server", host, got)
			}
			continue
		}
		exe, args, _ := strings.Cut(want, " ")
		if g := strings.ToLower(got); !ok || !strings.Contains(g, exe) || !strings.HasSuffix(got, args) {
			t.Errorf("%q: got %q, want ...%q", host, got, want)
		}
	}
}
