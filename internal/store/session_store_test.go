package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempHistory(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "hosts.json")
	orig := HistoryFile
	HistoryFile = func() (string, error) { return file, nil }
	t.Cleanup(func() { HistoryFile = orig })
	return file
}

func TestSessionsSaveUpdateDelete(t *testing.T) {
	requireSecretStore(t)
	useTempHistory(t)

	s, err := SaveSession(SavedSession{Host: " prod.example.com ", Port: 22, Login: "deploy", Group: "회사", Pass: "pw"}, true)
	if err != nil || s.ID == "" || s.Name != "prod.example.com" {
		t.Fatalf("save: %+v, %v", s, err)
	}
	_, _ = SaveSession(SavedSession{Name: "nas", Host: "192.168.0.2", Port: 23}, false)
	_, _ = SaveSession(SavedSession{Name: "db", Host: "db.example.com", Port: 22, Group: "회사", Pass: "x"}, false)

	list := LoadSessions()
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
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(must(HistoryFile())), "sessions.json"))
	if strings.Contains(string(data), `"pw"`) {
		t.Fatal("password stored in plain text")
	}

	// Saving with the same ID replaces it; without the password removes it.
	s.Name, s.Group = "운영 웹", "운영"
	if _, err := SaveSession(s, false); err != nil {
		t.Fatal(err)
	}
	list = LoadSessions()
	if len(list) != 3 || list[1].Name != "운영 웹" || list[1].Group != "운영" || list[1].Pass != "" {
		t.Fatalf("after update: %+v", list)
	}

	if err := DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if list = LoadSessions(); len(list) != 2 {
		t.Fatalf("after delete: %+v", list)
	}
	if _, err := SaveSession(SavedSession{Host: "  "}, false); err == nil {
		t.Fatal("empty host accepted")
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

// Prefs saved by an older version (in hosts.json) are still used.
func TestServerPrefsFromOldHistory(t *testing.T) {
	file := useTempHistory(t)
	old := `[{"host":"seoul","port":22,"login":"","encoding":"UTF-8","theme":"gruvbox","forwards":[{"type":"L","bindPort":15432,"host":"localhost","port":5432}]}]`
	if err := os.WriteFile(file, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := PrefsFor("seoul", 22); p.Theme != "gruvbox" || len(p.Forwards) != 1 {
		t.Fatalf("old prefs: %+v", p)
	}
	// Saving a session copies them over, so they survive the host leaving the history.
	_, _ = SaveSession(SavedSession{Host: "seoul", Port: 22}, false)
	_ = os.Remove(file)
	if p := PrefsFor("seoul", 22); p.Theme != "gruvbox" || len(p.Forwards) != 1 {
		t.Fatalf("copied prefs: %+v", p)
	}
}
