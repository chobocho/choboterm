package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// The window's path for a tab's output: base64 in term:data, which Wails
// then marshals to JSON for the page.
func BenchmarkOutputWindowPath(b *testing.B) {
	chunk := make([]byte, 32<<10)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := base64.StdEncoding.EncodeToString(chunk)
		if _, err := json.Marshal([]interface{}{1, s}); err != nil {
			b.Fatal(err)
		}
	}
}

// A web page's path: one binary frame (copy into a 5-byte header), queued
// for the page's writer; the page acknowledges as it goes.
func BenchmarkOutputWebPath(b *testing.B) {
	w := &webServer{app: NewApp(), opts: webOptions{Grace: webGrace}}
	c := newWebClient(w, 1, false)
	defer c.close()
	chunk := make([]byte, 32<<10)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.output(1<<tabBits+1, chunk)
		if i%64 == 63 { // the page acknowledges every so often
			c.mu.Lock()
			c.ackLocked(c.nextSeq - 1)
			c.mu.Unlock()
		}
	}
}

// What emitFor adds for the window's tabs: a test of the tab id. (No Wails
// context here, so the event itself goes nowhere.)
func BenchmarkEmitRoutingWindowTab(b *testing.B) {
	a := NewApp()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		a.emitFor(1, "xfer:progress", 1)
	}
}
