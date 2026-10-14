package app

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

// What emitFor adds for the window's tabs: a test of the tab id. (No Wails
// context here, so the event itself goes nowhere.)
func BenchmarkEmitRoutingWindowTab(b *testing.B) {
	a := NewApp()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		a.emitFor(1, "xfer:progress", 1)
	}
}
