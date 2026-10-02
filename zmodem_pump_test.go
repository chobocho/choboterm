package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// cmdSession runs a WSL command as if it were a remote shell.
type cmdSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
}

func (c *cmdSession) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *cmdSession) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *cmdSession) Resize(cols, rows int) error { return nil }
func (c *cmdSession) Close() error                { return c.cmd.Process.Kill() }

// TestZmodemThroughPump checks the whole path: sz output is detected in the
// terminal stream, the file is received, and terminal output resumes afterwards.
func TestZmodemThroughPump(t *testing.T) {
	bin := os.Getenv("CHOBOTERM_LRZSZ")
	if bin == "" {
		t.Skip("CHOBOTERM_LRZSZ not set")
	}
	payload := testPayload(200_000, 3)
	wslRun(t, "rm -rf /tmp/zp && mkdir -p /tmp/zp && cat > /tmp/zp/파일.bin", payload)

	cmd := exec.Command("wsl.exe", "--", "sh", "-c",
		"echo before; "+bin+"/sz -q /tmp/zp/파일.bin; echo after; sleep 1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var (
		mu     sync.Mutex
		screen strings.Builder
		closed = make(chan struct{})
	)
	dir := t.TempDir()
	a := NewApp()
	a.hooks.downloadDir = dir
	a.hooks.emit = func(name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(data[0].(string))
			screen.Write(b)
		case "term:closed":
			close(closed)
		}
	}
	sess := &cmdSession{cmd: cmd, stdin: stdin, stdout: stdout}
	a.sess = sess
	go a.pump(sess)

	select {
	case <-closed:
	case <-time.After(60 * time.Second):
		t.Fatal("timeout")
	}

	got, err := os.ReadFile(filepath.Join(dir, "파일.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("received %d bytes, err %v", len(got), err)
	}
	mu.Lock()
	out := screen.String()
	mu.Unlock()
	for _, want := range []string{"before", "1개 파일을 받았습니다", "after"} {
		if !strings.Contains(out, want) {
			t.Fatalf("screen missing %q:\n%q", want, out)
		}
	}
	if strings.Contains(out, "**\x18B") {
		t.Fatalf("ZMODEM header leaked to the screen:\n%q", out)
	}
}

// TestZmodemCancelThroughPump cancels a running sz with Ctrl+C from the keyboard.
func TestZmodemCancelThroughPump(t *testing.T) {
	bin := os.Getenv("CHOBOTERM_LRZSZ")
	if bin == "" {
		t.Skip("CHOBOTERM_LRZSZ not set")
	}
	wslRun(t, "rm -rf /tmp/zc && mkdir -p /tmp/zc && head -c 200000000 /dev/urandom > /tmp/zc/big.bin", nil)

	cmd := exec.Command("wsl.exe", "--", "sh", "-c", bin+"/sz -q /tmp/zc/big.bin; echo after-cancel; sleep 1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var (
		mu       sync.Mutex
		screen   strings.Builder
		progress = make(chan struct{}, 1)
		closed   = make(chan struct{})
	)
	dir := t.TempDir()
	a := NewApp()
	a.hooks.downloadDir = dir
	a.hooks.emit = func(name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(data[0].(string))
			screen.Write(b)
		case "xfer:progress":
			if data[0].(XferProgress).Done > 1_000_000 {
				select {
				case progress <- struct{}{}:
				default:
				}
			}
		case "term:closed":
			close(closed)
		}
	}
	sess := &cmdSession{cmd: cmd, stdin: stdin, stdout: stdout}
	a.sess = sess
	go a.pump(sess)

	select {
	case <-progress:
	case <-time.After(30 * time.Second):
		t.Fatal("transfer did not start")
	}
	a.Send("\x03")

	select {
	case <-closed:
	case <-time.After(60 * time.Second):
		t.Fatal("sz did not stop after cancel")
	}
	mu.Lock()
	out := screen.String()
	mu.Unlock()
	if !strings.Contains(out, "취소") || !strings.Contains(out, "after-cancel") {
		t.Fatalf("screen:\n%q", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("partial file left behind: %v", entries)
	}
}
