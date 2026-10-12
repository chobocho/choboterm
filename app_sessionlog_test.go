package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"choboterm/internal/config"
)

func TestSessionLogFollowsOutput(t *testing.T) {
	useTempSettings(t)
	dir := filepath.Join(t.TempDir(), "logs")
	if err := config.Update(func(s *config.Settings) { s.LogDir, s.LogTime, s.Language = dir, false, "ko" }); err != nil {
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
	if err := config.Update(func(s *config.Settings) { s.LogDir, s.LogRaw, s.LogTime, s.Language = dir, true, false, "ko" }); err != nil {
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
