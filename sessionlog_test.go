package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlainText(t *testing.T) {
	cases := []struct {
		name string
		in   []string // fed in pieces, to cross chunk boundaries
		want string
	}{
		{"lines", []string{"a\r\nb\n"}, "a\r\nb\r\n"},
		{"colors", []string{"\x1b[1;31mred\x1b[0m ok\r\n"}, "red ok\r\n"},
		{"split escape", []string{"x\x1b[3", "2my\r\n"}, "xy\r\n"},
		{"title", []string{"\x1b]0;user@host: ~\x07$ ls\r\n"}, "$ ls\r\n"},
		{"title ST", []string{"\x1b]2;t\x1b\\ok\r\n"}, "ok\r\n"},
		{"charset", []string{"\x1b(Bok\x1b=\r\n"}, "ok\r\n"},
		{"progress", []string{" 10%\r 50%\r100%\r\n"}, "100%\r\n"},
		{"backspace", []string{"lx\b \bs\r\n"}, "ls\r\n"},
		{"erase line", []string{"abcdef\r\x1b[Kxy\r\n"}, "xy\r\n"},
		{"korean split", []string{"한\xea", "\xb8\x80\r\n"}, "한글\r\n"},
		{"blank lines", []string{"a\r\n\r\nb\r\n"}, "a\r\n\r\nb\r\n"},
		{"bell", []string{"a\x07b\r\n"}, "ab\r\n"},
	}
	for _, c := range cases {
		p := &plainText{}
		var got []byte
		for _, s := range c.in {
			got = append(got, p.feed([]byte(s))...)
		}
		got = append(got, p.rest()...)
		if string(got) != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLogFileName(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 5, 7, 0, time.Local)
	if got := logFileName("example.com", 22, at); got != "example.com_22_20261003_090507.log" {
		t.Fatal(got)
	}
	if got := logFileName("::1", 2222, at); got != "__1_2222_20261003_090507.log" {
		t.Fatal(got)
	}
	if got := logFileName("", 0, at); got != "session_20261003_090507.log" {
		t.Fatal(got)
	}
}

// readLog waits until the log file contains want and returns its contents.
func readLog(t *testing.T, path, want string) string {
	t.Helper()
	var s string
	for i := 0; i < 200; i++ {
		b, _ := os.ReadFile(path)
		if s = string(b); strings.Contains(s, want) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log %q does not contain %q", s, want)
	return ""
}

func TestSessionLogFollowsOutput(t *testing.T) {
	useTempSettings(t)
	dir := filepath.Join(t.TempDir(), "logs")
	if err := updateSettings(func(s *Settings) { s.LogDir = dir }); err != nil {
		t.Fatal(err)
	}
	var changed []string
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "log:changed" {
			changed = append(changed, data[1].(string))
		}
	}
	tb := a.getTab(1)
	sess := newPipeSession()
	tb.sess, tb.host, tb.port = sess, "srv", 23
	go tb.pump(sess)

	path, err := a.StartLog(1)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "srv_23_") {
		t.Fatalf("path %q", path)
	}
	if again, _ := a.StartLog(1); again != path || a.LogPath(1) != path {
		t.Fatalf("second start gave %q", again)
	}

	_, _ = sess.out.Write([]byte("\x1b[32mhello\x1b[0m\r\n$ "))
	readLog(t, path, "hello\r\n")

	a.CloseTab(1)
	s := readLog(t, path, "연결을 끊었습니다")
	if !strings.HasPrefix(s, "=====") || !strings.Contains(s, "로그 시작: srv:23") ||
		!strings.Contains(s, "hello\r\n$\r\n=====") || strings.Contains(s, "\x1b") {
		t.Fatalf("log:\n%s", s)
	}
	if a.LogPath(1) != "" {
		t.Fatal("still logging after the tab closed")
	}
	if len(changed) != 1 || changed[0] != path {
		t.Fatalf("log:changed events %q", changed)
	}
}

func TestSessionLogRawKeepsEscapes(t *testing.T) {
	useTempSettings(t)
	dir := t.TempDir()
	if err := updateSettings(func(s *Settings) { s.LogDir, s.LogRaw = dir, true }); err != nil {
		t.Fatal(err)
	}
	var changed []string
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "log:changed" {
			changed = append(changed, data[1].(string))
		}
	}
	tb := a.getTab(1)
	sess := newPipeSession()
	tb.sess = sess
	go tb.pump(sess)
	path, err := a.StartLog(1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sess.out.Write([]byte("\x1b[31mred\x1b[0m"))
	readLog(t, path, "\x1b[31mred\x1b[0m")
	a.StopLog(1)
	a.CloseTab(1)
	// Nothing more is written once logging stopped.
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "연결을 끊었습니다") {
		t.Fatalf("written after stop: %q", b)
	}
	if len(changed) != 2 || changed[1] != "" {
		t.Fatalf("log:changed events %q", changed)
	}
}
