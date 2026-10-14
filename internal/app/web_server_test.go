package app

// Tests of the web server alone: no App behind it (nopHost).

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// nopHost is a webHost without an app.
type nopHost struct{}

func (nopHost) Invoke(*webClient, string, []json.RawMessage) (interface{}, error) {
	return nil, errors.New("no app")
}
func (nopHost) ClientClosed(int)         {}
func (nopHost) HasWindow() bool          { return false }
func (nopHost) ShowWindow()              {}
func (nopHost) ServerStopped(*webServer) {}

// bareServer is a server that isn't listening, for tests of its clients.
func bareServer(t testing.TB) *webServer {
	return &webServer{
		host: nopHost{}, opts: webOptions{Grace: webGrace}, stageRoot: t.TempDir(),
		clients: map[int]*webClient{}, codes: map[string]time.Time{},
		stages: map[string]*webStage{}, downloads: map[string]*webDownload{},
	}
}

func TestStagePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "st")
	for rel, ok := range map[string]bool{
		"a.txt": true, "d/b.txt": true, `d\c.txt`: true, "/lead.txt": true,
		"": false, "..": false, "../x": false, "d/../../x": false, "a\x00b": false,
	} {
		p, got := stagePath(dir, rel)
		if got != ok {
			t.Errorf("%q: %v", rel, got)
		}
		if got && !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			t.Errorf("%q -> %q", rel, p)
		}
	}
}

func TestSafeFileNameAndZip(t *testing.T) {
	for in, want := range map[string]string{"a.txt": "a.txt", "../x": "x", `C:\a\b.txt`: "b.txt", "a:b?.txt": "a_b_.txt", "": "f", "..": "f"} {
		if got := safeFileName(in, "f"); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "d"), 0o755)
	os.WriteFile(filepath.Join(src, "한글.txt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(src, "d", "x.txt"), []byte("22"), 0o644)
	zp := filepath.Join(t.TempDir(), "o.zip")
	if err := zipFolder(src, zp); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(zp)
	if !bytes.Contains(b, []byte("한글.txt")) || !bytes.Contains(b, []byte("d/x.txt")) {
		t.Fatal("zip entries")
	}
}

// Output waits while the page is too far behind, and goes on once it acknowledges.
func TestWebOutputBackpressure(t *testing.T) {
	old := webMaxUnacked
	webMaxUnacked = 1000
	defer func() { webMaxUnacked = old }()
	c, _ := bareServer(t).newClient(false)
	chunk := make([]byte, 600)
	c.output(c.id<<tabBits+1, chunk) // 605 bytes queued
	c.output(c.id<<tabBits+1, chunk) // over the limit after this one
	done := make(chan struct{})
	go func() { c.output(c.id<<tabBits+1, chunk); close(done) }()
	select {
	case <-done:
		t.Fatal("output didn't wait")
	case <-time.After(100 * time.Millisecond):
	}
	c.mu.Lock()
	c.ackLocked(2)
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output still waiting after the ack")
	}
	// Events never wait.
	c.event("x", nil)
	c.close() // and closing releases any waiting writer
}

// A web page's path: one binary frame (copy into a 5-byte header), queued
// for the page's writer; the page acknowledges as it goes.
func BenchmarkOutputWebPath(b *testing.B) {
	c, _ := bareServer(b).newClient(false)
	defer c.close()
	chunk := make([]byte, 32<<10)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.output(c.id<<tabBits+1, chunk)
		if i%64 == 63 { // the page acknowledges every so often
			c.mu.Lock()
			c.ackLocked(c.nextSeq - 1)
			c.mu.Unlock()
		}
	}
}
