package app

import (
	"fmt"
	"path/filepath"
	"testing"

	"choboterm/internal/store"
)

func TestTabThemeSavedPerHost(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "hosts.json")
	orig := store.HistoryFile
	store.HistoryFile = func() (string, error) { return hist, nil }
	defer func() { store.HistoryFile = orig }()

	a := NewApp()
	if ok, err := a.SetTabTheme(1, "dracula"); ok || err != nil {
		t.Fatalf("never connected tab: %v %v", ok, err)
	}
	_ = store.AddHistory(store.HostEntry{Host: "busan", Port: 22, Login: "me"}, nil)
	_ = store.AddHistory(store.HostEntry{Host: "seoul", Port: 22}, nil)
	tb := a.getTab(1)
	tb.host, tb.port = "busan", 22

	if ok, err := a.SetTabTheme(1, "dracula"); !ok || err != nil {
		t.Fatalf("set: %v %v", ok, err)
	}
	// Connecting again keeps it; other hosts are not touched.
	_ = store.AddHistory(store.HostEntry{Host: "busan", Port: 22, Login: "me"}, nil)
	if got := a.TabTheme(1); got != "dracula" {
		t.Fatalf("theme = %q", got)
	}
	for _, h := range store.LoadHistory() {
		if want := map[string]string{"busan": "dracula"}[h.Host]; store.PrefsFor(h.Host, h.Port).Theme != want {
			t.Fatalf("%s theme = %q", h.Host, store.PrefsFor(h.Host, h.Port).Theme)
		}
	}
	// "" goes back to the Settings theme.
	_, _ = a.SetTabTheme(1, "")
	if got := a.TabTheme(1); got != "" {
		t.Fatalf("after reset: %q", got)
	}
}

// The theme and forwarding rules of a server used to be lost when it
// dropped out of the last 20 hosts.
func TestServerPrefsOutliveHistory(t *testing.T) {
	useTempHosts(t)
	a := NewApp()
	_ = store.AddHistory(store.HostEntry{Host: "busan", Port: 22}, nil)
	tb := a.getTab(1)
	tb.host, tb.port = "busan", 22
	if ok, err := a.SetTabTheme(1, "dracula"); !ok || err != nil {
		t.Fatalf("set: %v %v", ok, err)
	}
	_ = store.SetHistoryForwards("busan", 22, []store.Forward{{Type: "D", BindPort: 1080}})

	for i := 0; i < store.MaxHistory+5; i++ {
		_ = store.AddHistory(store.HostEntry{Host: fmt.Sprintf("h%d", i), Port: 22}, nil)
	}
	if _, ok := store.HistoryEntry("busan", 22); ok {
		t.Fatal("busan should have dropped out of the history")
	}
	if got := a.TabTheme(1); got != "dracula" {
		t.Fatalf("theme = %q", got)
	}
	if f := store.HistoryForwards("busan", 22); len(f) != 1 || f[0].BindPort != 1080 {
		t.Fatalf("forwards = %+v", f)
	}
}

// useTempHosts keeps a test's history (hosts.json) in a folder of its own.
func useTempHosts(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "hosts.json")
	orig := store.HistoryFile
	store.HistoryFile = func() (string, error) { return file, nil }
	t.Cleanup(func() { store.HistoryFile = orig })
	return file
}
