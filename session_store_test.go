package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempHistory(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "hosts.json")
	orig := historyFile
	historyFile = func() (string, error) { return file, nil }
	t.Cleanup(func() { historyFile = orig })
	return file
}

func TestSessionsSaveUpdateDelete(t *testing.T) {
	useTempHistory(t)

	s, err := saveSession(SavedSession{Host: " prod.example.com ", Port: 22, Login: "deploy", Group: "회사", Pass: "pw"}, true)
	if err != nil || s.ID == "" || s.Name != "prod.example.com" {
		t.Fatalf("save: %+v, %v", s, err)
	}
	_, _ = saveSession(SavedSession{Name: "nas", Host: "192.168.0.2", Port: 23}, false)
	_, _ = saveSession(SavedSession{Name: "db", Host: "db.example.com", Port: 22, Group: "회사", Pass: "x"}, false)

	list := loadSessions()
	var names []string
	for _, e := range list {
		names = append(names, e.Group+"/"+e.Name)
	}
	// Top level first, then groups; names sorted inside a group.
	if got := strings.Join(names, ","); got != "/nas,회사/db,회사/prod.example.com" {
		t.Fatalf("order %s", got)
	}
	if list[2].Pass != "pw" || list[1].Pass != "" {
		t.Fatalf("passwords: %+v", list)
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(must(historyFile())), "sessions.json"))
	if strings.Contains(string(data), `"pw"`) {
		t.Fatal("password stored in plain text")
	}

	// Saving with the same ID replaces it; without the password removes it.
	s.Name, s.Group = "운영 웹", "운영"
	if _, err := saveSession(s, false); err != nil {
		t.Fatal(err)
	}
	list = loadSessions()
	if len(list) != 3 || list[1].Name != "운영 웹" || list[1].Group != "운영" || list[1].Pass != "" {
		t.Fatalf("after update: %+v", list)
	}

	if err := deleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if list = loadSessions(); len(list) != 2 {
		t.Fatalf("after delete: %+v", list)
	}
	if _, err := saveSession(SavedSession{Host: "  "}, false); err == nil {
		t.Fatal("empty host accepted")
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

// The theme and forwarding rules of a server used to be lost when it
// dropped out of the last 20 hosts.
func TestServerPrefsOutliveHistory(t *testing.T) {
	useTempHistory(t)
	a := NewApp()
	_ = addHistory(HostEntry{Host: "busan", Port: 22}, nil)
	tb := a.getTab(1)
	tb.host, tb.port = "busan", 22
	if ok, err := a.SetTabTheme(1, "dracula"); !ok || err != nil {
		t.Fatalf("set: %v %v", ok, err)
	}
	_ = setHistoryForwards("busan", 22, []Forward{{Type: "D", BindPort: 1080}})

	for i := 0; i < maxHistory+5; i++ {
		_ = addHistory(HostEntry{Host: fmt.Sprintf("h%d", i), Port: 22}, nil)
	}
	if _, ok := historyEntry("busan", 22); ok {
		t.Fatal("busan should have dropped out of the history")
	}
	if got := a.TabTheme(1); got != "dracula" {
		t.Fatalf("theme = %q", got)
	}
	if f := historyForwards("busan", 22); len(f) != 1 || f[0].BindPort != 1080 {
		t.Fatalf("forwards = %+v", f)
	}
}

// Prefs saved by an older version (in hosts.json) are still used.
func TestServerPrefsFromOldHistory(t *testing.T) {
	file := useTempHistory(t)
	old := `[{"host":"seoul","port":22,"login":"","encoding":"UTF-8","theme":"gruvbox","forwards":[{"type":"L","bindPort":15432,"host":"localhost","port":5432}]}]`
	if err := os.WriteFile(file, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := prefsFor("seoul", 22); p.Theme != "gruvbox" || len(p.Forwards) != 1 {
		t.Fatalf("old prefs: %+v", p)
	}
	// Saving a session copies them over, so they survive the host leaving the history.
	_, _ = saveSession(SavedSession{Host: "seoul", Port: 22}, false)
	_ = os.Remove(file)
	if p := prefsFor("seoul", 22); p.Theme != "gruvbox" || len(p.Forwards) != 1 {
		t.Fatalf("copied prefs: %+v", p)
	}
}
