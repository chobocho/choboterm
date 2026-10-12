package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"choboterm/internal/store"
)

// Without a window (headless web, or a test) no method may reach the Wails
// runtime: with no window it calls log.Fatal and the server would die. So
// every bound method is called here from a web page, with zero arguments,
// on an App that has no Wails context.
func TestEveryMethodWithoutWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateSettings(t)
	sess := filepath.Join(t.TempDir(), "sessions.json")
	orig := store.SessionsFile
	store.SessionsFile = func() (string, error) { return sess, nil }
	t.Cleanup(func() { store.SessionsFile = orig })
	hist := filepath.Join(t.TempDir(), "hosts.json")
	origH := store.HistoryFile
	store.HistoryFile = func() (string, error) { return hist, nil }
	t.Cleanup(func() { store.HistoryFile = origH })

	f := startTestWeb(t, webOptions{})
	for _, local := range []bool{false, true} {
		c, err := f.w.newClient(local)
		if err != nil {
			t.Fatal(err)
		}
		c.close() // a file chooser for a closed page is cancelled at once
		var names []string
		for n := range webMethods {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = webInvoke(c.view, name, []json.RawMessage{})
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Errorf("%s (local=%v) didn't return", name, local)
			}
		}
	}
	if f.a.quit.Load() {
		t.Fatal("a page quit the app")
	}
}
